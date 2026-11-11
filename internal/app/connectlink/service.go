// Package connectlink lets a workspace member hand a client a link through
// which the client connects their own mailboxes, by Google or Microsoft
// sign-in or by server details, without a Warmbly account and without sharing
// a password with the workspace. Every connect goes through the same email
// service paths the dashboard uses, in the name of the member who made the link.
package connectlink

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/formwire"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/climit"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

const (
	// oauthStateTTL bounds a client's round trip through the provider.
	oauthStateTTL = 10 * time.Minute
	// attemptWindow and attemptLimit bound sign-in starts and credential checks per link across every replica.
	attemptWindow = time.Hour
	attemptLimit  = 30
	maxTags       = 20
	listMax       = 100
)

// Mailboxes is the slice of the email service a connect link drives; nothing here forks a connect path.
type Mailboxes interface {
	OAuthAuthorizeURL(provider models.InboxProvider, state, verifier string) (string, *errx.Error)
	OAuthCallbackOrigin(provider models.InboxProvider) (string, *errx.Error)
	OAuthConnectWithCode(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider, code, verifier string) (*models.Email, *errx.Error)
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	BulkUpdateTags(ctx context.Context, orgID string, emailIDs, addTags, removeTags []uuid.UUID) (int, *errx.Error)
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
}

// Workspaces answers who may still connect in a workspace's name and how much room it has.
type Workspaces interface {
	Get(ctx context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error)
	RequirePermission(ctx context.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) *errx.Error
	MailboxAllowance(ctx context.Context, orgID uuid.UUID) (*models.MailboxAllowance, *errx.Error)
}

// WarmupScheduler seeds a mailbox's warmup chain once warmup is switched on.
type WarmupScheduler interface {
	EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error
}

// Notifier tells the link's creator a client connected a mailbox.
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
}

// CaptchaVerifier is the Turnstile check on a server-details submit.
type CaptchaVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) *errx.Error
}

// Deps wires the service. Warmup, Notifier, Captcha and Redis may be nil.
type Deps struct {
	Repo       repository.ClientConnectLinkRepository
	Mailboxes  Mailboxes
	Workspaces Workspaces
	Warmup     WarmupScheduler
	Notifier   Notifier
	Captcha    CaptchaVerifier
	Redis      *redis.Client
	// Brands is what the connect page shows; nil is the deployment's own brand.
	Brands brandkit.Resolver
	// BaseURL is the origin a workspace's connect pages are served on; nil is the shared portal address.
	BaseURL func(ctx context.Context, orgID uuid.UUID) string
	Now     func() time.Time
	// Vendors connects an inbox vendor account a client names; nil leaves that way in off.
	Vendors Vendors
}

// WireVendors turns on connecting an inbox vendor account from a link; the two services are built apart.
func (s *Service) WireVendors(v Vendors) { s.d.Vendors = v }

type Service struct {
	d      Deps
	states stateStore
}

func New(d Deps) *Service {
	if d.Brands == nil {
		d.Brands = brandkit.DeploymentResolver()
	}
	if d.BaseURL == nil {
		d.BaseURL = func(context.Context, uuid.UUID) string { return config.PortalBaseURL() }
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &Service{d: d}
	if d.Redis != nil {
		s.states = redisStates{d.Redis}
	}
	return s
}

func (s *Service) withStatus(l *models.ClientConnectLink) *models.ClientConnectLink {
	if l != nil {
		l.Status = l.StatusAt(s.d.Now())
	}
	return l
}

// Create mints a link and returns its URL, which is never shown again.
func (s *Service) Create(ctx context.Context, orgID, userID uuid.UUID, in models.NewClientConnectLink) (*models.CreatedClientConnectLink, *errx.Error) {
	base := s.d.BaseURL(ctx, orgID)
	if base == "" {
		return nil, ErrUnavailable
	}
	label, xerr := displayname.Validate("Label", in.Label, displayname.Workspace, true)
	if xerr != nil {
		return nil, xerr
	}
	providers, xerr := normalizeProviders(in.Providers)
	if xerr != nil {
		return nil, xerr
	}
	if in.MaxMailboxes < 1 || in.MaxMailboxes > models.ClientConnectLinkMaxMailboxes {
		return nil, ErrInvalidMax
	}
	days := in.ExpiresInDays
	if days == 0 {
		days = models.ClientConnectLinkDefaultDays
	}
	if days < 1 || days > models.ClientConnectLinkMaxDays {
		return nil, ErrInvalidExpiry
	}
	if len(in.TagIDs) > maxTags {
		return nil, ErrTooManyTags
	}
	rawTags, xerr := validate.Uuids(in.TagIDs)
	if xerr != nil {
		return nil, xerr
	}
	tags := make([]uuid.UUID, 0, len(rawTags))
	for _, t := range rawTags {
		tags = append(tags, uuid.MustParse(t))
	}

	token, hash, err := newToken()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	creator := userID
	link, xerr := s.d.Repo.Create(ctx, &models.ClientConnectLink{
		OrganizationID:   orgID,
		CreatedBy:        &creator,
		Label:            label,
		TokenHash:        hash,
		AllowedProviders: providers,
		MaxMailboxes:     in.MaxMailboxes,
		TagIDs:           tags,
		Warmup:           in.Warmup,
		ExpiresAt:        s.d.Now().Add(time.Duration(days) * 24 * time.Hour),
	})
	if xerr != nil {
		return nil, xerr
	}
	return &models.CreatedClientConnectLink{Link: *s.withStatus(link), URL: URL(base, token)}, nil
}

// normalizeProviders dedupes the chosen providers into the stored order.
func normalizeProviders(in []string) ([]string, *errx.Error) {
	want := map[string]bool{}
	for _, p := range in {
		want[strings.TrimSpace(p)] = true
	}
	out := make([]string, 0, len(models.ConnectLinkProviders))
	for _, p := range models.ConnectLinkProviders {
		if want[string(p)] {
			out = append(out, string(p))
			delete(want, string(p))
		}
	}
	if len(out) == 0 || len(want) > 0 {
		return nil, ErrInvalidProviders
	}
	return out, nil
}

// List is the workspace's links, newest first.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*models.ClientConnectLinkList, *errx.Error) {
	if limit < 1 || limit > listMax {
		return nil, errx.ErrLimit
	}
	afterAt, afterID, xerr := paging.DecodeTimeCursor(cursor)
	if xerr != nil {
		return nil, xerr
	}
	links, xerr := s.d.Repo.List(ctx, orgID, afterAt, afterID, limit+1)
	if xerr != nil {
		return nil, xerr
	}
	out := &models.ClientConnectLinkList{Data: links}
	if len(links) > limit {
		out.Data = links[:limit]
		last := out.Data[limit-1]
		out.Pagination = models.CPagination{HasMore: true, NextCursor: paging.EncodeTime(last.CreatedAt, last.ID)}
	}
	for i := range out.Data {
		s.withStatus(&out.Data[i])
	}
	return out, nil
}

// Uses lists the mailboxes connected through one of the workspace's links.
func (s *Service) Uses(ctx context.Context, orgID, id uuid.UUID) ([]models.ClientConnectLinkUse, *errx.Error) {
	link, xerr := s.d.Repo.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if link == nil {
		return nil, errx.ErrNotFound
	}
	return s.d.Repo.ListUses(ctx, orgID, id, models.ClientConnectLinkMaxMailboxes)
}

// Revoke turns a link off for good. Mailboxes already connected stay connected.
func (s *Service) Revoke(ctx context.Context, orgID, id uuid.UUID) (*models.ClientConnectLink, *errx.Error) {
	link, xerr := s.d.Repo.Revoke(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if link == nil {
		return nil, errx.ErrNotFound
	}
	return s.withStatus(link), nil
}

// Resolve finds the live link behind a URL token, refusing a dead one with the reason it is dead.
func (s *Service) Resolve(ctx context.Context, token string) (*models.ClientConnectLink, *errx.Error) {
	hash, ok := hashToken(token)
	if !ok {
		return nil, ErrNotFound
	}
	link, xerr := s.d.Repo.ByTokenHash(ctx, hash)
	if xerr != nil {
		return nil, xerr
	}
	if link == nil {
		return nil, ErrNotFound
	}
	s.withStatus(link)
	if xerr := statusError(link.Status); xerr != nil {
		return link, xerr
	}
	return link, nil
}

// Page is what the public connect page renders for a live link.
func (s *Service) Page(ctx context.Context, link *models.ClientConnectLink) *formwire.ConnectLink {
	orgID := link.OrganizationID
	brand, err := s.d.Brands.ForOrganization(ctx, orgID)
	if err != nil {
		log.Warn().Err(err).Str("organization_id", orgID.String()).Msg("connect link: brand unavailable, showing the default")
		brand = brandkit.Default()
	}
	workspace := ""
	if org, xerr := s.d.Workspaces.Get(ctx, orgID); xerr == nil && org != nil {
		workspace = org.Name
	}
	out := &formwire.ConnectLink{
		Workspace:    displayname.DisplayableOr(workspace, "Your agency"),
		Brand:        brand,
		Providers:    s.availableProviders(link),
		MaxMailboxes: link.MaxMailboxes,
		Connected:    link.MailboxesConnected,
		ExpiresAt:    link.ExpiresAt,
	}
	if a, xerr := s.d.Workspaces.MailboxAllowance(ctx, orgID); xerr == nil && a != nil && !a.CanAdd(1) {
		out.WorkspaceFull = true
	}
	if s.d.Captcha != nil && config.CaptchaProvider() != "none" && (link.Allows(models.InboxProviderSMTPIMAP) || link.Allows(models.ConnectLinkVendor)) {
		out.CaptchaSiteKey = config.TurnstileSiteKey()
	}
	if link.Allows(models.ConnectLinkVendor) && s.d.Vendors != nil {
		out.Vendors = s.vendorCatalog()
	}
	return out
}

// availableProviders is what the link allows and this instance can actually do.
func (s *Service) availableProviders(link *models.ClientConnectLink) []string {
	out := []string{}
	for _, p := range models.ConnectLinkProviders {
		if link.Allows(p) && s.providerAvailable(p) {
			out = append(out, string(p))
		}
	}
	return out
}

func (s *Service) providerAvailable(p models.InboxProvider) bool {
	switch p {
	case models.InboxProviderSMTPIMAP:
		return true
	case models.ConnectLinkVendor:
		return s.d.Vendors != nil
	case models.InboxProviderGoogle:
		if !config.GoogleOAuthConnect() {
			return false
		}
	}
	_, xerr := s.d.Mailboxes.OAuthCallbackOrigin(p)
	return xerr == nil
}

// allowAttempt spends one of the link's attempts; a Redis outage fails open like the sync governor's budgets.
func (s *Service) allowAttempt(ctx context.Context, linkID uuid.UUID) *errx.Error {
	if s.d.Redis == nil {
		return nil
	}
	n, err := climit.Limit(ctx, s.d.Redis, "connect_link_attempts:"+linkID.String(), attemptWindow)
	if err != nil {
		return nil
	}
	if n > attemptLimit {
		return ErrRateLimited
	}
	return nil
}

// creatorCanConnect holds the link to its creator still managing the workspace's mailboxes.
func (s *Service) creatorCanConnect(ctx context.Context, link *models.ClientConnectLink) (uuid.UUID, *errx.Error) {
	if link.CreatedBy == nil {
		return uuid.Nil, ErrRevoked
	}
	if xerr := s.d.Workspaces.RequirePermission(ctx, link.OrganizationID, *link.CreatedBy, models.PermManageEmails); xerr != nil {
		if xerr.Code == errx.Forbidden || xerr.Code == errx.NotFound {
			return uuid.Nil, ErrRevoked
		}
		return uuid.Nil, xerr
	}
	return *link.CreatedBy, nil
}

// admit refuses a connect the link or the workspace cannot take before anything is asked of the client.
func (s *Service) admit(ctx context.Context, link *models.ClientConnectLink, provider models.InboxProvider) *errx.Error {
	if xerr := statusError(link.StatusAt(s.d.Now())); xerr != nil {
		return xerr
	}
	if !link.Allows(provider) {
		return ErrProviderNotAllowed
	}
	if !s.providerAvailable(provider) {
		return ErrProviderUnavailable
	}
	if _, xerr := s.creatorCanConnect(ctx, link); xerr != nil {
		return xerr
	}
	if a, xerr := s.d.Workspaces.MailboxAllowance(ctx, link.OrganizationID); xerr != nil {
		return xerr
	} else if a != nil && !a.CanAdd(1) {
		return errx.MailboxAllowanceReached(a.Used, allowanceOf(a), a.Paid)
	}
	return s.allowAttempt(ctx, link.ID)
}

func allowanceOf(a *models.MailboxAllowance) int {
	if a.Allowance == nil {
		return 0
	}
	return *a.Allowance
}

// Result is a mailbox a client connected, and the member it was connected for.
type Result struct {
	Account *models.Email
	Link    *models.ClientConnectLink
	ActorID uuid.UUID
}

// connect takes a slot, runs one of the email service's connect paths in the creator's name, and records the use.
func (s *Service) connect(ctx context.Context, link *models.ClientConnectLink, provider models.InboxProvider, ipHashed string,
	run func(userID string) (*models.Email, *errx.Error)) (*Result, *errx.Error) {
	actor, xerr := s.creatorCanConnect(ctx, link)
	if xerr != nil {
		return nil, xerr
	}
	reserved, xerr := s.d.Repo.Reserve(ctx, link.OrganizationID, link.ID)
	if xerr != nil {
		return nil, xerr
	}
	if !reserved {
		fresh, gerr := s.d.Repo.Get(ctx, link.OrganizationID, link.ID)
		if gerr != nil || fresh == nil {
			return nil, ErrRevoked
		}
		if se := statusError(fresh.StatusAt(s.d.Now())); se != nil {
			return nil, se
		}
		return nil, ErrExhausted
	}
	acc, xerr := run(actor.String())
	if xerr != nil || acc == nil {
		if rerr := s.d.Repo.Release(ctx, link.OrganizationID, link.ID); rerr != nil {
			log.Warn().Str("link_id", link.ID.String()).Msg("connect link: could not give back a slot after a failed connect")
		}
		if xerr == nil {
			xerr = errx.InternalError()
		}
		return nil, xerr
	}
	link.MailboxesConnected++
	accID := acc.ID
	use := &models.ClientConnectLinkUse{LinkID: link.ID, EmailAccountID: &accID, Email: acc.Email, Provider: string(provider)}
	if xerr := s.d.Repo.RecordUse(ctx, use, link.OrganizationID, ipHashed); xerr != nil {
		log.Warn().Str("link_id", link.ID.String()).Msg("connect link: mailbox connected but its use was not recorded")
	}
	s.applyDefaults(ctx, link, acc)
	s.notifyCreator(ctx, link, actor, acc)
	return &Result{Account: acc, Link: s.withStatus(link), ActorID: actor}, nil
}

// applyDefaults puts the link's tags and warmup choice on a new mailbox; best-effort, the mailbox is connected either way.
func (s *Service) applyDefaults(ctx context.Context, link *models.ClientConnectLink, acc *models.Email) {
	orgID := link.OrganizationID.String()
	if len(link.TagIDs) > 0 {
		if _, xerr := s.d.Mailboxes.BulkUpdateTags(ctx, orgID, []uuid.UUID{acc.ID}, link.TagIDs, nil); xerr != nil {
			log.Warn().Str("link_id", link.ID.String()).Str("email_account_id", acc.ID.String()).Msg("connect link: tags not applied")
		}
	}
	if !link.Warmup {
		return
	}
	if _, xerr := s.d.Mailboxes.SetWarmupLifecycle(ctx, orgID, acc.ID.String(), "start"); xerr != nil {
		log.Warn().Str("link_id", link.ID.String()).Str("email_account_id", acc.ID.String()).Str("reason", xerr.ResponseCode()).
			Msg("connect link: warmup not started")
		return
	}
	if s.d.Warmup != nil {
		_ = s.d.Warmup.EnsureWarmupScheduled(ctx, acc.ID)
	}
}

func (s *Service) notifyCreator(ctx context.Context, link *models.ClientConnectLink, creator uuid.UUID, acc *models.Email) {
	if s.d.Notifier == nil {
		return
	}
	via := "your connect link"
	if label := displayname.Displayable(link.Label); label != "" {
		via = "your connect link \"" + label + "\""
	}
	orgID := link.OrganizationID
	s.d.Notifier.Notify(ctx, creator, &orgID, models.NotifTeamActivity,
		"A client connected a mailbox",
		// The address is the client's words, so it stays out of a body that may be mailed; the mailbox list shows it.
		"A mailbox was connected through "+via+". Open Mailboxes to see it.",
		"/app/emails",
		map[string]any{"email_account_id": acc.ID.String(), "connect_link_id": link.ID.String()})
}

// SMTPIMAPInput is a client's server details, with the signals only the portal service sees.
type SMTPIMAPInput struct {
	Email        string
	Name         string
	SMTP         *models.Service
	IMAP         *models.Service
	CaptchaToken string
	RemoteIP     string
}

// ConnectSMTPIMAP checks a client's server details live and connects the mailbox; the credentials are sealed by the same path the dashboard uses.
func (s *Service) ConnectSMTPIMAP(ctx context.Context, link *models.ClientConnectLink, in SMTPIMAPInput) (*Result, *errx.Error) {
	if s.d.Captcha != nil {
		if xerr := s.d.Captcha.Verify(ctx, in.CaptchaToken, in.RemoteIP); xerr != nil {
			return nil, ErrCaptcha
		}
	}
	if xerr := s.admit(ctx, link, models.InboxProviderSMTPIMAP); xerr != nil {
		return nil, xerr
	}
	orgID := link.OrganizationID
	return s.connect(ctx, link, models.InboxProviderSMTPIMAP, ipHash(link.ID, in.RemoteIP), func(userID string) (*models.Email, *errx.Error) {
		return s.d.Mailboxes.OnboardSMTPIMAP(ctx, userID, &orgID, &models.NewSMTPIMAPAccount{
			Email: in.Email, Name: in.Name, SMTP: in.SMTP, IMAP: in.IMAP,
		})
	})
}
