package partner

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/repository"
)

// Errors in the partner API's vocabulary.
var (
	ErrUnauthorized     = errx.NewWithIdentifier(errx.Unauthorized, "unauthorized", "A valid partner key is required.")
	ErrPartnerSuspended = errx.NewWithIdentifier(errx.Forbidden, "partner_suspended", "This partner account is suspended.")
	ErrNotFound         = errx.NewWithIdentifier(errx.NotFound, "not_found", "Organization not found.")
	ErrPlanUnknown      = errx.NewWithIdentifier(errx.Unprocessable, "plan_unknown", "That plan is not available to this partner.")
	ErrOrgCancelled     = errx.NewWithIdentifier(errx.Conflict, "organization_cancelled", "This organization is cancelled.")
	ErrOrgSuspended     = errx.NewWithIdentifier(errx.Conflict, "organization_suspended", "This organization is suspended.")
	ErrOrgDeleting      = &errx.Error{Code: errx.ServiceUnavailable, Identifier: "organization_deleting", Message: "This organization is being deleted. Try again shortly.", RetryAfter: 30}
)

func invalid(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_request", msg)
}

func internal(err error) *errx.Error {
	errs.CaptureException(err)
	return errx.InternalError()
}

// OrgStore is the membership surface of the organization repository.
type OrgStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Organization, error)
	GetMember(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, error)
	ListRoles(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationRole, error)
	GetMemberRoles(ctx context.Context, orgID, userID uuid.UUID) ([]models.MemberRole, error)
	AddMember(ctx context.Context, member *models.OrganizationMember) error
	AddMemberWithRoles(ctx context.Context, member *models.OrganizationMember, roleIDs []uuid.UUID) error
	UpdateMember(ctx context.Context, member *models.OrganizationMember) error
	SetMemberRoles(ctx context.Context, orgID, userID uuid.UUID, roleIDs []uuid.UUID) error
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error
}

// Counter reads the organization's usage counts.
type Counter interface {
	GetOrganizationCounts(ctx context.Context, orgID uuid.UUID) (*models.OrganizationCounts, *errx.Error)
}

// Balances reads the organization's balance.
type Balances interface {
	GetBalance(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)
}

// APIKeys creates and revokes workspace keys.
type APIKeys interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateAPIKey) (*models.APIKeyWithSecret, *errx.Error)
	Get(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, *errx.Error)
	List(ctx context.Context, orgID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeysResult, *errx.Error)
	Revoke(ctx context.Context, orgID, keyID uuid.UUID, reason string) *errx.Error
}

// Sessions mints login tokens and sessions and ends sessions.
type Sessions interface {
	GenerateTokenFor(purpose string, userID, sessionID uuid.UUID, email, nonce string, issuedAt, expiresAt time.Time) (string, error)
	VerifyTokenFor(purpose, tokenStr string) (*token.TokenClaims, *errx.Error)
	GenerateSessionWithOrg(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string, orgID *uuid.UUID) (*models.Token, *errx.Error)
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) *errx.Error
}

// Bans reads a user's ban scope.
type Bans interface {
	GetBanState(ctx context.Context, userID uuid.UUID) (uint32, error)
}

// Deletions schedules and cancels the delayed deletion of a workspace.
type Deletions interface {
	ScheduleOrganizationDeletion(ctx context.Context, orgID, requesterUserID uuid.UUID, req *models.ScheduleDeletionRequest) (*models.ScheduledDeletion, *errx.Error)
	CancelOrganizationDeletion(ctx context.Context, orgID, requesterUserID uuid.UUID, req *models.CancelDeletionRequest) *errx.Error
}

// Sealer seals and opens secrets with the instance key (CREDENTIALS_ENCRYPTION_KEY).
type Sealer interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(sealed string) (string, error)
}

// Auditor records workspace audit entries, which also refresh teammates' dashboards.
type Auditor interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// CodeStore keeps one-time login codes; Take reads and deletes in one step.
type CodeStore interface {
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Take(ctx context.Context, key string) ([]byte, error)
}

// EnabledCounter counts a workspace's mailboxes per enablement (built with per-mailbox enablement).
type EnabledCounter interface {
	EnabledCounts(ctx context.Context, orgID uuid.UUID) (EnabledCounts, error)
}

// Deps wires the service. Repo is the only required field; the rest are nil-safe where noted.
type Deps struct {
	Repo      repository.PartnerRepository
	Orgs      OrgStore
	Counts    Counter
	Balances  Balances
	APIKeys   APIKeys
	Sessions  Sessions
	Bans      Bans
	Deletions Deletions
	Sealer    Sealer
	Audit     Auditor
	Codes     CodeStore
	Cache     *cache.Cache
	HTTP      *http.Client
	// Seeders run after a managed workspace is created, best-effort.
	Seeders []func(ctx context.Context, orgID uuid.UUID)
}

type Service struct {
	Deps
	enabled EnabledCounter
	mb      MailboxDeps

	mu     sync.Mutex
	states map[uuid.UUID]cachedState
	// linked is set once any partner workspace is seen; noLinksAt is when none was last confirmed.
	linked    bool
	noLinksAt time.Time
	now       func() time.Time
}

func NewService(d Deps) *Service {
	if d.HTTP == nil {
		d.HTTP = safehttp.Client(10 * time.Second)
	}
	return &Service{Deps: d, states: map[uuid.UUID]cachedState{}, now: time.Now}
}

// WireEnabledCounter supplies usage.enabled once per-mailbox enablement exists.
func (s *Service) WireEnabledCounter(c EnabledCounter) { s.enabled = c }

// Caller is the partner and mode behind one partner API request.
type Caller struct {
	Partner *models.Partner
	Mode    models.PartnerMode
	KeyID   uuid.UUID
}

type callerKey struct{}

func WithCaller(ctx context.Context, c *Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom is the partner a request authenticated as, or nil.
func CallerFrom(ctx context.Context) *Caller {
	c, _ := ctx.Value(callerKey{}).(*Caller)
	return c
}

// Authenticate resolves a partner key. The mode comes from the key itself.
func (s *Service) Authenticate(ctx context.Context, raw string) (*Caller, *errx.Error) {
	mode, ok := KeyMode(raw)
	if !ok || len(raw) > 128 {
		return nil, ErrUnauthorized
	}
	p, k, err := s.Repo.AuthenticatePartnerKey(ctx, HashKey(raw))
	if err != nil {
		return nil, internal(err)
	}
	if p == nil || k == nil || k.Mode != mode {
		return nil, ErrUnauthorized
	}
	if p.Status != models.PartnerStatusActive {
		return nil, ErrPartnerSuspended
	}
	if err := s.Repo.TouchPartnerKey(ctx, k.ID); err != nil {
		errs.CaptureException(err)
	}
	return &Caller{Partner: p, Mode: mode, KeyID: k.ID}, nil
}

// Allow is the partner's rate limit, a one-second window shared through the cache. Fails open.
func (s *Service) Allow(ctx context.Context, c *Caller) bool {
	if s.Cache == nil || c == nil {
		return true
	}
	limit := c.Partner.RateLimitPerSecond
	if limit <= 0 {
		limit = 50
	}
	// One budget for the partner, across all of its keys and both modes.
	key := "partner:rl:" + c.Partner.ID.String() + ":" + s.now().UTC().Format("150405")
	n, err := s.Cache.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		_ = s.Cache.Expire(ctx, key, 2*time.Second).Err()
	}
	return n <= int64(limit)
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)

// CreatePartnerInput is what an operator gives a new partner.
type CreatePartnerInput struct {
	Slug, Name, DisplayName string
	AllowedHosts            []string
}

// CreatePartner adds a partner and its service account. It holds no key until one is created.
func (s *Service) CreatePartner(ctx context.Context, in CreatePartnerInput, by *uuid.UUID) (*models.Partner, *errx.Error) {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugPattern.MatchString(slug) {
		return nil, invalid("slug is 2-40 lowercase letters, digits and dashes.")
	}
	name, xerr := displayname.Validate("Name", in.Name, displayname.Heading, false)
	if xerr != nil {
		return nil, xerr
	}
	display := in.DisplayName
	if strings.TrimSpace(display) == "" {
		display = name
	}
	display, xerr = displayname.Validate("Display name", display, displayname.Heading, false)
	if xerr != nil {
		return nil, xerr
	}
	hosts, xerr := normalizeHosts(in.AllowedHosts)
	if xerr != nil {
		return nil, xerr
	}
	p, err := s.Repo.CreatePartner(ctx, &models.Partner{Slug: slug, Name: name, DisplayName: display, AllowedHosts: hosts},
		serviceEmail(slug), by)
	if errors.Is(err, repository.ErrPartnerSlugTaken) {
		return nil, errx.NewWithIdentifier(errx.Conflict, "partner_exists", "A partner with that slug already exists.")
	}
	if err != nil {
		return nil, internal(err)
	}
	return p, nil
}

// serviceEmail addresses the account a partner's workspaces are owned by; nothing is ever delivered to it.
func serviceEmail(slug string) string {
	return "service-" + randomHex(8) + "@" + slug + ".partner.invalid"
}

// personEmail addresses a partner-bound account; the person's real address is kept on the identity.
func personEmail(slug string) string {
	return "p-" + randomHex(12) + "@" + slug + ".partner.invalid"
}

func normalizeHosts(in []string) ([]string, *errx.Error) {
	out := []string{}
	seen := map[string]bool{}
	for _, h := range in {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if strings.ContainsAny(h, "/:@?# ") || !strings.Contains(h, ".") && h != "localhost" {
			return nil, invalid("allowed hosts are bare hostnames, like app.example.com.")
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if len(out) > 20 {
		return nil, invalid("at most 20 allowed hosts.")
	}
	return out, nil
}

// normalizeDomains lowercases and dedupes the bare domains an operator allows a partner to delegate.
func normalizeDomains(in []string) ([]string, *errx.Error) {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range in {
		d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
		if d == "" {
			continue
		}
		if len(d) > 253 || strings.ContainsAny(d, "/:@?#* ") || !strings.Contains(d, ".") {
			return nil, invalid("delegation domains are bare domains, like example.com.")
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) > 500 {
		return nil, invalid("at most 500 delegation domains.")
	}
	return out, nil
}

// UpdatePartnerInput changes what an operator set; nil leaves a field alone.
type UpdatePartnerInput struct {
	Name, DisplayName         *string
	Status                    *models.PartnerStatus
	AllowedHosts              *[]string
	DelegationDomains         *[]string
	WebhookURL                *string
	CallbackBaseURL           *string
	WholesaleSendingWarmupDay *int64
	WholesaleWarmupOnlyDay    *int64
	RateLimitPerSecond        *int
}

func (s *Service) UpdatePartner(ctx context.Context, id uuid.UUID, in UpdatePartnerInput) (*models.Partner, *errx.Error) {
	var u repository.PartnerUpdate
	if in.Name != nil {
		v, xerr := displayname.Validate("Name", *in.Name, displayname.Heading, false)
		if xerr != nil {
			return nil, xerr
		}
		u.Name = &v
	}
	if in.DisplayName != nil {
		v, xerr := displayname.Validate("Display name", *in.DisplayName, displayname.Heading, false)
		if xerr != nil {
			return nil, xerr
		}
		u.DisplayName = &v
	}
	if in.Status != nil {
		if *in.Status != models.PartnerStatusActive && *in.Status != models.PartnerStatusSuspended {
			return nil, invalid("status is active or suspended.")
		}
		u.Status = in.Status
	}
	if in.AllowedHosts != nil {
		hosts, xerr := normalizeHosts(*in.AllowedHosts)
		if xerr != nil {
			return nil, xerr
		}
		u.AllowedHosts, u.SetAllowedHosts = hosts, true
	}
	if in.DelegationDomains != nil {
		domains, xerr := normalizeDomains(*in.DelegationDomains)
		if xerr != nil {
			return nil, xerr
		}
		u.DelegationDomains, u.SetDelegationDomains = domains, true
	}
	for _, f := range []struct {
		v     *string
		field string
	}{{in.WebhookURL, "webhook_url"}, {in.CallbackBaseURL, "callback_base_url"}} {
		if f.v == nil || strings.TrimSpace(*f.v) == "" {
			continue
		}
		if err := webhook.ValidateOutboundURL(*f.v); err != nil {
			return nil, invalid(f.field + ": " + err.Error())
		}
	}
	if in.WebhookURL != nil {
		v := strings.TrimSpace(*in.WebhookURL)
		u.WebhookURL = &v
	}
	if in.CallbackBaseURL != nil {
		v := strings.TrimRight(strings.TrimSpace(*in.CallbackBaseURL), "/")
		u.CallbackBaseURL = &v
	}
	for _, m := range []*int64{in.WholesaleSendingWarmupDay, in.WholesaleWarmupOnlyDay} {
		if m != nil && (*m < 0 || *m > 1_000_000_000) {
			return nil, invalid("wholesale prices are 0 to 1,000 USD a mailbox-day, in micro-USD.")
		}
	}
	u.WholesaleSendingWarmupDay, u.WholesaleWarmupOnlyDay = in.WholesaleSendingWarmupDay, in.WholesaleWarmupOnlyDay
	if in.RateLimitPerSecond != nil {
		if *in.RateLimitPerSecond < 20 || *in.RateLimitPerSecond > 1000 {
			return nil, invalid("rate_limit_per_second is 20 to 1000.")
		}
		u.RateLimitPerSecond = in.RateLimitPerSecond
	}
	p, err := s.Repo.UpdatePartner(ctx, id, u)
	if err != nil {
		return nil, internal(err)
	}
	if p == nil {
		return nil, errx.ErrNotFound
	}
	return p, nil
}

// RotateWebhookSecret issues a new signing secret, shown once; the previous one keeps signing for a day.
func (s *Service) RotateWebhookSecret(ctx context.Context, id uuid.UUID) (string, *errx.Error) {
	if s.Sealer == nil {
		return "", errx.NewPublic(errx.ServiceUnavailable, "CREDENTIALS_ENCRYPTION_KEY is not configured.")
	}
	secret := "whsec_" + randomHex(32)
	sealed, err := s.Sealer.Encrypt(secret)
	if err != nil {
		return "", internal(err)
	}
	if err := s.Repo.RotatePartnerWebhookSecret(ctx, id, sealed); err != nil {
		return "", internal(err)
	}
	return secret, nil
}

// SetCallbackKey stores the key the partner issued for its token callback; "" clears it.
func (s *Service) SetCallbackKey(ctx context.Context, id uuid.UUID, key string) *errx.Error {
	key = strings.TrimSpace(key)
	sealed := ""
	if key != "" {
		if s.Sealer == nil {
			return errx.NewPublic(errx.ServiceUnavailable, "CREDENTIALS_ENCRYPTION_KEY is not configured.")
		}
		var err error
		if sealed, err = s.Sealer.Encrypt(key); err != nil {
			return internal(err)
		}
	}
	if err := s.Repo.SetPartnerCallbackKey(ctx, id, sealed); err != nil {
		return internal(err)
	}
	return nil
}

// CallbackKey opens the partner's token-callback key, "" when none is set.
func (s *Service) CallbackKey(ctx context.Context, partnerID uuid.UUID) (string, error) {
	sec, err := s.Repo.GetPartnerSecrets(ctx, partnerID)
	if err != nil || sec == nil || sec.CallbackKey == "" || s.Sealer == nil {
		return "", err
	}
	return s.Sealer.Decrypt(sec.CallbackKey)
}

// CreateKey mints a partner key; the secret exists only in the answer.
func (s *Service) CreateKey(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, name string, by *uuid.UUID) (*models.PartnerAPIKeyWithSecret, *errx.Error) {
	if !mode.Valid() {
		return nil, invalid("mode is live or test.")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = string(mode) + " key"
	}
	if len(name) > 120 {
		return nil, invalid("name is at most 120 characters.")
	}
	p, err := s.Repo.GetPartner(ctx, partnerID)
	if err != nil {
		return nil, internal(err)
	}
	if p == nil {
		return nil, errx.ErrNotFound
	}
	raw, prefix, suffix, hash, err := GenerateKey(mode)
	if err != nil {
		return nil, internal(err)
	}
	k := models.PartnerAPIKey{ID: uuid.New(), PartnerID: partnerID, Mode: mode, Name: name, KeyPrefix: prefix, KeySuffix: suffix}
	if err := s.Repo.CreatePartnerKey(ctx, &k, hash, by); err != nil {
		return nil, internal(err)
	}
	return &models.PartnerAPIKeyWithSecret{PartnerAPIKey: k, Secret: raw}, nil
}

func (s *Service) RevokeKey(ctx context.Context, partnerID, keyID uuid.UUID) *errx.Error {
	ok, err := s.Repo.RevokePartnerKey(ctx, partnerID, keyID)
	if err != nil {
		return internal(err)
	}
	if !ok {
		return errx.ErrNotFound
	}
	return nil
}
