// Package mailboxconnect connects mailboxes for a workspace that drives
// Warmbly through its API: SMTP/IMAP rows with warmup started, and single-use
// sign-in links a Google or Microsoft mailbox owner opens to connect theirs.
package mailboxconnect

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

const (
	// LinkPath is where a sign-in link opens, on this deployment's API origin.
	LinkPath = "/addresses/link/"
	// StatePrefix marks a provider round trip a link started.
	StatePrefix = "mc."
	// WarmupFailedCode marks a connected link whose mailbox did not start warming.
	WarmupFailedCode = "connect_warmup_failed"
	// WarmupSettingsFailedCode marks a connected link whose mailbox warms on its default ramp, the link's settings refused.
	WarmupSettingsFailedCode = "connect_warmup_settings_failed"

	DefaultLinkHours = 72
	MaxLinkHours     = 168
	stateTTL         = 15 * time.Minute
	finishBudget     = 5 * time.Minute
	listMax          = 100
	listDefault      = 25
)

var (
	ErrLinkGone      = errx.NewWithIdentifier(errx.NotFound, "connect_link_unavailable", "This sign-in link has expired, was revoked, or already connected its mailbox. Ask for a new one.")
	ErrLinkNotFound  = errx.NewWithIdentifier(errx.NotFound, "connect_link_not_found", "Sign-in link not found.")
	ErrLinkBrowser   = errx.NewWithIdentifier(errx.Forbidden, "connect_link_browser", "Finish the sign-in in the browser that opened the link. Open the link again to restart.")
	ErrLinkProvider  = errx.NewWithIdentifier(errx.BadRequest, "connect_link_provider", "provider must be gmail or outlook.")
	ErrLinkEmail     = errx.NewWithIdentifier(errx.BadRequest, "connect_link_email", "email must be a single email address.")
	ErrLinkReturnURL = errx.NewWithIdentifier(errx.BadRequest, "connect_link_return_url", "return_url must be an https address with no fragment.")
	ErrLinkExpiry    = errx.NewWithIdentifier(errx.BadRequest, "connect_link_expiry", fmt.Sprintf("expires_in_hours must be between 1 and %d.", MaxLinkHours))
	ErrCursor        = errx.NewWithIdentifier(errx.BadRequest, "invalid_cursor", "cursor is not one this endpoint issued.")
	ErrLimit         = errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", fmt.Sprintf("limit must be between 1 and %d.", listMax))
	ErrWarmup        = errx.NewWithIdentifier(errx.BadRequest, "connect_warmup", "warmup values must be positive, and reply_rate between 1 and 100.")
	ErrPlanRequired  = errx.NewWithIdentifier(errx.Forbidden, "partnership_plan_required", "This workspace is no longer on the Partnership plan, so this sign-in link cannot connect a mailbox.")
)

// Mailboxes is the mailbox service surface a connect needs; email.EmailService satisfies it.
type Mailboxes interface {
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	OnboardSMTPIMAPBulk(ctx context.Context, userID string, orgID *uuid.UUID, rows []models.NewSMTPIMAPAccount) *models.MailboxBulkResult
	Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
	LinkSigninURL(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider, state, verifier, loginHint string) (string, *uuid.UUID, *errx.Error)
	LinkSigninFinish(ctx context.Context, userID string, orgID uuid.UUID, provider models.InboxProvider, appID *uuid.UUID, code, verifier, expectedEmail string) (*models.Email, *errx.Error)
	ConnectWithGrant(ctx context.Context, userID string, orgID uuid.UUID, g models.MailboxOAuthGrant, allow func(uuid.UUID) bool) (*models.Email, bool, *errx.Error)
}

// WarmupScheduler seeds a mailbox's warmup chain; tasks.TasksService satisfies it.
type WarmupScheduler interface {
	EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error
}

// Authority re-checks, when a link completes, that its creator may still connect mailboxes.
type Authority interface {
	RequirePermission(ctx context.Context, orgID, userID uuid.UUID, perm models.OrganizationPermission) *errx.Error
}

// Describer names a link's workspace and sign-in app for the page its owner sees.
type Describer interface {
	WorkspaceName(ctx context.Context, orgID uuid.UUID) string
	AppName(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) string
}

// LinkView is what the landing page shows the mailbox owner.
type LinkView struct {
	Provider  models.InboxProvider
	Email     string
	Workspace string
	App       string
}

// Finished is a completed or refused round trip, and where to send the browser after it.
type Finished struct {
	Link    *models.MailboxConnectLink
	Account *models.Email
	// WarmupNote says what of the link's warmup did not apply; empty when it all did.
	WarmupNote string
	ReturnURL  string
}

// Service connects mailboxes through the API and sign-in links.
type Service struct {
	links     repository.MailboxConnectLinkRepository
	mailboxes Mailboxes
	warmup    WarmupScheduler
	authority Authority
	describe  Describer
	tools     PartnerTools
	cache     *cache.Cache
	// publicBase is this deployment's public API origin, where links open.
	publicBase string
	now        func() time.Time
}

// New returns the service.
func New(links repository.MailboxConnectLinkRepository, mailboxes Mailboxes, warmup WarmupScheduler, authority Authority, describe Describer, c *cache.Cache, publicBase string) *Service {
	return &Service{links: links, mailboxes: mailboxes, warmup: warmup, authority: authority, describe: describe,
		cache: c, publicBase: strings.TrimRight(publicBase, "/"), now: time.Now}
}

func internalErr(err error, msg string) *errx.Error {
	errs.CaptureException(fmt.Errorf("%s: %w", msg, err))
	return errx.InternalError()
}

// ValidateWarmup refuses values the mailbox settings would refuse later, before anything connects.
func ValidateWarmup(w *models.MailboxConnectWarmup) *errx.Error {
	if w == nil {
		return nil
	}
	for _, v := range []*int{w.Start, w.Max, w.Increase} {
		if v != nil && *v < 1 {
			return ErrWarmup
		}
	}
	if w.ReplyRate != nil && (*w.ReplyRate < 1 || *w.ReplyRate > 100) {
		return ErrWarmup
	}
	return nil
}

// ApplyWarmup starts a new mailbox's warmup and returns it as stored (nil when nothing was written) and what failed.
func (s *Service) ApplyWarmup(ctx context.Context, orgID uuid.UUID, userID string, accountID uuid.UUID, w *models.MailboxConnectWarmup) (*models.Email, string) {
	acc, notes, _ := s.applyWarmup(ctx, orgID, userID, accountID, w)
	return acc, strings.Join(notes.all(), " ")
}

// warmupNotes says what of a mailbox's warmup did not apply, settings and start apart.
type warmupNotes struct{ settings, start string }

func (n warmupNotes) all() []string {
	var out []string
	for _, s := range []string{n.settings, n.start} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// publicMessage keeps an internal failure's detail out of what a caller or mailbox owner reads.
func publicMessage(xerr *errx.Error) string {
	if xerr.Code == errx.Internal && !xerr.Public {
		return "Something went wrong on our side."
	}
	return xerr.Message
}

func (s *Service) applyWarmup(ctx context.Context, orgID uuid.UUID, userID string, accountID uuid.UUID, w *models.MailboxConnectWarmup) (*models.Email, warmupNotes, bool) {
	var notes warmupNotes
	if w == nil || !w.Enabled {
		return nil, notes, false
	}
	var latest *models.Email
	if w.Start != nil || w.Max != nil || w.Increase != nil || w.ReplyRate != nil {
		upd := models.UpdateEmail{WarmupBase: w.Start, WarmupMax: w.Max, WarmupIncrease: w.Increase, WarmupReplyRate: w.ReplyRate}
		if acc, xerr := s.mailboxes.Update(ctx, orgID.String(), userID, accountID.String(), &upd); xerr != nil {
			notes.settings = "Warmup settings not applied: " + publicMessage(xerr)
		} else if acc != nil && acc.ID == accountID {
			latest = acc
		}
	}
	acc, xerr := s.mailboxes.SetWarmupLifecycle(ctx, orgID.String(), accountID.String(), "start")
	if xerr != nil {
		notes.start = "Warmup not started: " + publicMessage(xerr)
	} else if acc != nil && acc.ID == accountID {
		latest = acc
	}
	if xerr == nil && s.warmup != nil {
		if err := s.warmup.EnsureWarmupScheduled(ctx, accountID); err != nil {
			errs.CaptureException(fmt.Errorf("seed warmup after connect: %w", err))
		}
	}
	return latest, notes, xerr == nil
}

// ConnectSMTP connects one SMTP/IMAP mailbox and starts its warmup when asked.
func (s *Service) ConnectSMTP(ctx context.Context, orgID uuid.UUID, userID string, data *models.NewSMTPIMAPAccount, w *models.MailboxConnectWarmup) (*models.Email, string, *errx.Error) {
	if xerr := ValidateWarmup(w); xerr != nil {
		return nil, "", xerr
	}
	acc, xerr := s.mailboxes.OnboardSMTPIMAP(ctx, userID, &orgID, data)
	if xerr != nil {
		return nil, "", xerr
	}
	stored, note := s.ApplyWarmup(ctx, orgID, userID, acc.ID, w)
	if stored != nil {
		acc = stored
	}
	return acc, note, nil
}

// ConnectSMTPBulk connects many SMTP/IMAP mailboxes and starts warmup on each one connected.
func (s *Service) ConnectSMTPBulk(ctx context.Context, orgID uuid.UUID, userID string, rows []models.NewSMTPIMAPAccount, w *models.MailboxConnectWarmup) (*models.MailboxBulkResult, *errx.Error) {
	if xerr := ValidateWarmup(w); xerr != nil {
		return nil, xerr
	}
	result := s.mailboxes.OnboardSMTPIMAPBulk(ctx, userID, &orgID, rows)
	for i := range result.Data {
		r := &result.Data[i]
		if r.Status != models.MailboxBulkConnected || r.ID == nil {
			continue
		}
		if _, note := s.ApplyWarmup(ctx, orgID, userID, *r.ID, w); note != "" {
			r.Message = note
		}
	}
	return result, nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func validReturnURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && !strings.Contains(raw, "#")
}

// CreateLink mints a sign-in link; its URL is returned only here.
func (s *Service) CreateLink(ctx context.Context, orgID, userID uuid.UUID, req models.NewMailboxConnectLink) (*models.MailboxConnectLink, *errx.Error) {
	if !models.ValidMailboxOAuthAppProvider(req.Provider) {
		return nil, ErrLinkProvider
	}
	email := strings.TrimSpace(req.Email)
	if email != "" {
		if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 320 {
			return nil, ErrLinkEmail
		}
	}
	returnURL := strings.TrimSpace(req.ReturnURL)
	if returnURL != "" && !validReturnURL(returnURL) {
		return nil, ErrLinkReturnURL
	}
	hours := req.ExpiresInHours
	if hours == 0 {
		hours = DefaultLinkHours
	}
	if hours < 1 || hours > MaxLinkHours {
		return nil, ErrLinkExpiry
	}
	if xerr := ValidateWarmup(req.Warmup); xerr != nil {
		return nil, xerr
	}
	// The sign-in client is checked now, so a link that could never connect is not handed out.
	if _, _, xerr := s.mailboxes.LinkSigninURL(ctx, orgID, req.Provider, "check", oauth2.GenerateVerifier(), email); xerr != nil {
		return nil, xerr
	}

	raw, err := crypt.Nonce()
	if err != nil {
		return nil, internalErr(err, "connect link nonce")
	}
	link := &models.MailboxConnectLink{
		OrganizationID: orgID, CreatedBy: userID, Provider: req.Provider, ExpectedEmail: email,
		ReturnURL: returnURL, ExpiresAt: s.now().Add(time.Duration(hours) * time.Hour).UTC(),
	}
	if req.Warmup != nil {
		link.Warmup = *req.Warmup
	}
	saved, err := s.links.CreateConnectLink(ctx, link, hashToken(raw))
	if err != nil {
		return nil, internalErr(err, "create connect link")
	}
	saved.URL = s.publicBase + LinkPath + raw
	saved.Status = saved.EffectiveStatus(s.now())
	return saved, nil
}

func (s *Service) present(l *models.MailboxConnectLink) *models.MailboxConnectLink {
	l.Status = l.EffectiveStatus(s.now())
	return l
}

// GetLink returns one of the workspace's links.
func (s *Service) GetLink(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxConnectLink, *errx.Error) {
	l, err := s.links.GetConnectLink(ctx, orgID, id)
	if err != nil {
		return nil, internalErr(err, "get connect link")
	}
	if l == nil {
		return nil, ErrLinkNotFound
	}
	return s.present(l), nil
}

func encodeCursor(l *models.MailboxConnectLink) string {
	return base64.RawURLEncoding.EncodeToString([]byte(l.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + l.ID.String()))
}

func decodeCursor(raw string) (*repository.ConnectLinkCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, false
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, false
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, false
	}
	return &repository.ConnectLinkCursor{CreatedAt: t, ID: u}, true
}

// ListLinks pages the workspace's links newest first; next is empty on the last page.
func (s *Service) ListLinks(ctx context.Context, orgID uuid.UUID, cursor string, limit int) ([]*models.MailboxConnectLink, string, *errx.Error) {
	if limit == 0 {
		limit = listDefault
	}
	if limit < 1 || limit > listMax {
		return nil, "", ErrLimit
	}
	var after *repository.ConnectLinkCursor
	if cursor != "" {
		c, ok := decodeCursor(cursor)
		if !ok {
			return nil, "", ErrCursor
		}
		after = c
	}
	rows, err := s.links.ListConnectLinks(ctx, orgID, after, limit+1)
	if err != nil {
		return nil, "", internalErr(err, "list connect links")
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(rows[len(rows)-1])
	}
	for _, l := range rows {
		s.present(l)
	}
	return rows, next, nil
}

// RevokeLink ends a link that has not connected its mailbox.
func (s *Service) RevokeLink(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxConnectLink, *errx.Error) {
	if _, err := s.links.RevokeConnectLink(ctx, orgID, id); err != nil {
		return nil, internalErr(err, "revoke connect link")
	}
	return s.GetLink(ctx, orgID, id)
}

// live resolves a link's secret to a link still waiting for its mailbox.
func (s *Service) live(ctx context.Context, token string) (*models.MailboxConnectLink, *errx.Error) {
	if len(token) < 16 || len(token) > 128 {
		return nil, ErrLinkGone
	}
	l, err := s.links.GetConnectLinkByToken(ctx, hashToken(token))
	if err != nil {
		return nil, internalErr(err, "get connect link by token")
	}
	if l == nil {
		return nil, ErrLinkGone
	}
	switch l.EffectiveStatus(s.now()) {
	case models.ConnectLinkPending, models.ConnectLinkConnecting:
		return l, nil
	}
	return nil, ErrLinkGone
}

// Describe is what the landing page shows before the owner chooses to continue.
func (s *Service) Describe(ctx context.Context, token string) (*LinkView, *errx.Error) {
	l, xerr := s.live(ctx, token)
	if xerr != nil {
		return nil, xerr
	}
	v := &LinkView{Provider: l.Provider, Email: l.ExpectedEmail}
	if s.describe != nil {
		v.Workspace = s.describe.WorkspaceName(ctx, l.OrganizationID)
		v.App = s.describe.AppName(ctx, l.OrganizationID, l.Provider)
	}
	return v, nil
}

func bindingHash(binding string) string {
	sum := sha256.Sum256([]byte(binding))
	return hex.EncodeToString(sum[:])
}

func stateKey(state string) string { return "mailbox_connect:" + state }

// IsState reports a provider callback that a sign-in link started.
func IsState(state string) bool { return strings.HasPrefix(state, StatePrefix) }

// Continue binds a round trip to the browser's cookie and returns the provider's sign-in URL.
func (s *Service) Continue(ctx context.Context, token, binding string) (string, *errx.Error) {
	if len(binding) < 16 {
		return "", ErrLinkBrowser
	}
	l, xerr := s.live(ctx, token)
	if xerr != nil {
		return "", xerr
	}
	if s.cache == nil {
		return "", errx.InternalError()
	}
	nonce, err := crypt.Nonce()
	if err != nil {
		return "", internalErr(err, "connect state nonce")
	}
	state := StatePrefix + nonce
	verifier := oauth2.GenerateVerifier()
	to, appID, xerr := s.mailboxes.LinkSigninURL(ctx, l.OrganizationID, l.Provider, state, verifier, l.ExpectedEmail)
	if xerr != nil {
		return "", xerr
	}
	st := models.MailboxConnectLinkState{LinkID: l.ID, OrgID: l.OrganizationID, OAuthAppID: appID, Verifier: verifier, BindingHash: bindingHash(binding)}
	if err := s.cache.SetJSON(ctx, stateKey(state), st, stateTTL); err != nil {
		return "", internalErr(err, "save connect state")
	}
	return to, nil
}

// Finish completes a round trip in the browser that started it; a refused attempt leaves the link usable until it expires.
func (s *Service) Finish(ctx context.Context, code, state, providerErr, binding string) (*Finished, *errx.Error) {
	// The whole round trip ends well inside the stale-claim window, so a revoke never lands before the mailbox does.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishBudget)
	defer cancel()
	if s.cache == nil {
		return nil, errx.InternalError()
	}
	raw, err := s.cache.GetDel(ctx, stateKey(state)).Bytes()
	if err != nil {
		return nil, ErrLinkGone
	}
	var st models.MailboxConnectLinkState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, ErrLinkGone
	}
	if binding == "" || subtle.ConstantTimeCompare([]byte(bindingHash(binding)), []byte(st.BindingHash)) != 1 {
		return nil, ErrLinkBrowser
	}
	l, err := s.links.ClaimConnectLink(ctx, st.LinkID)
	if err != nil {
		return nil, internalErr(err, "claim connect link")
	}
	if l == nil || l.OrganizationID != st.OrgID {
		return nil, ErrLinkGone
	}
	fail := func(xerr *errx.Error) (*Finished, *errx.Error) {
		code := "connect_failed"
		if xerr != nil {
			code = xerr.ResponseCode()
		}
		if err := s.links.ReleaseConnectLink(ctx, l.ID, code); err != nil {
			errs.CaptureException(fmt.Errorf("release connect link: %w", err))
		}
		l.Status, l.ErrorCode = models.ConnectLinkPending, code
		return &Finished{Link: l, ReturnURL: s.returnTo(l, "failed", code)}, xerr
	}
	if providerErr != "" {
		return fail(errx.NewWithIdentifier(errx.BadRequest, "connect_link_declined", "The sign-in was cancelled. Open the link again to retry."))
	}
	if s.authority != nil {
		if xerr := s.authority.RequirePermission(ctx, l.OrganizationID, l.CreatedBy, models.PermManageEmails); xerr != nil {
			return fail(errx.NewWithIdentifier(errx.Forbidden, "connect_link_creator_left", "Whoever created this link can no longer connect mailboxes to the workspace. Ask for a new link."))
		}
	}
	// The link is held to the same plan as the API that made it.
	if s.tools != nil {
		if ok, xerr := s.tools.CanUsePartnerTools(ctx, l.OrganizationID); xerr != nil {
			return fail(xerr)
		} else if !ok {
			return fail(ErrPlanRequired)
		}
	}
	acc, xerr := s.mailboxes.LinkSigninFinish(ctx, l.CreatedBy.String(), l.OrganizationID, l.Provider, st.OAuthAppID, code, st.Verifier, l.ExpectedEmail)
	if xerr != nil {
		return fail(xerr)
	}
	warmup := l.Warmup
	stored, notes, started := s.applyWarmup(ctx, l.OrganizationID, l.CreatedBy.String(), acc.ID, &warmup)
	if stored != nil {
		acc = stored
	}
	note := strings.Join(notes.all(), " ")
	warning := ""
	switch {
	case warmup.Enabled && !started:
		warning = WarmupFailedCode
	case notes.settings != "":
		warning = WarmupSettingsFailedCode
	}
	s.completeLink(ctx, l.ID, acc.ID, warning)
	now := s.now()
	id := acc.ID
	l.Status, l.ErrorCode, l.EmailAccountID, l.CompletedAt = models.ConnectLinkConnected, warning, &id, &now
	return &Finished{Link: l, Account: acc, WarmupNote: note, ReturnURL: s.returnTo(l, "connected", warning)}, nil
}

// returnTo is the workspace's own page with the outcome appended; empty when the link names none.
func (s *Service) returnTo(l *models.MailboxConnectLink, status, code string) string {
	if l == nil || l.ReturnURL == "" {
		return ""
	}
	u, err := url.Parse(l.ReturnURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("warmbly_link", l.ID.String())
	q.Set("status", status)
	if code != "" {
		q.Set("code", code)
	}
	if l.EmailAccountID != nil {
		q.Set("email_account_id", l.EmailAccountID.String())
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// completeLink records the link's mailbox, retried briefly; the mailbox exists whether or not it is recorded.
func (s *Service) completeLink(ctx context.Context, linkID, accountID uuid.UUID, warning string) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = s.links.CompleteConnectLink(ctx, linkID, accountID, warning); err == nil || errors.Is(err, repository.ErrConnectLinkNotRecorded) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	if err != nil {
		errs.CaptureException(fmt.Errorf("complete connect link %s for mailbox %s: %w", linkID, accountID, err))
	}
}

// PartnerTools reports a workspace that may use the Partnership tools; feature.FeatureGateService satisfies it.
type PartnerTools interface {
	CanUsePartnerTools(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// WirePartnerTools holds a link's completion to the plan its API requires.
func (s *Service) WirePartnerTools(t PartnerTools) { s.tools = t }
