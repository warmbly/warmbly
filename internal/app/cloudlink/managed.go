package cloudlink

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Cloud-managed mailboxes: the grant lives on Warmbly Cloud; this instance sends with brokered access tokens.

var (
	ErrNotManaged   = errx.NewWithIdentifier(errx.NotFound, "cloud_link_not_managed", "This mailbox is not managed by Warmbly Cloud.")
	ErrOAuthSession = errx.NewWithIdentifier(errx.NotFound, "cloud_link_oauth_session", "That sign-in session is unknown or has expired. Start again.")
)

// OAuthReturnPath is the dashboard route the cloud sends the popup back to.
const OAuthReturnPath = "/cloud-oauth/done"

// tokenCacheMax bounds how long a brokered token is reused before the cloud is asked again.
const tokenCacheMax = 10 * time.Minute

type oauthSession struct {
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Provider  models.InboxProvider
	ExpiresAt time.Time
}

type cachedToken struct {
	token   *models.PoolLinkAccessToken
	expires time.Time
}

func (s *service) StartOAuth(ctx context.Context, orgID, userID uuid.UUID, provider models.InboxProvider) (*models.CloudLinkOAuthStart, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	if provider != models.InboxProviderGoogle && provider != models.InboxProviderOutlook {
		return nil, errx.ErrEmailOnboardProvider
	}
	var res models.PoolLinkOAuthStartResponse
	req := models.PoolLinkOAuthStartRequest{Provider: provider, ReturnURL: strings.TrimRight(config.AppBaseURL(), "/") + OAuthReturnPath}
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/oauth/start", req, &res); xerr != nil {
		return nil, xerr
	}
	s.mu.Lock()
	s.sessions[res.Session] = oauthSession{OrgID: orgID, UserID: userID, Provider: provider, ExpiresAt: time.Now().Add(15 * time.Minute)}
	for k, v := range s.sessions {
		if time.Now().After(v.ExpiresAt) {
			delete(s.sessions, k)
		}
	}
	s.mu.Unlock()
	return &models.CloudLinkOAuthStart{URL: res.URL, Session: res.Session}, nil
}

func (s *service) FinishOAuth(ctx context.Context, orgID, userID uuid.UUID, session string) (*models.Email, *errx.Error) {
	s.mu.Lock()
	sess, ok := s.sessions[session]
	s.mu.Unlock()
	if !ok || sess.OrgID != orgID || time.Now().After(sess.ExpiresAt) {
		return nil, ErrOAuthSession
	}
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var state models.PoolLinkMailboxState
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/oauth/finish", models.PoolLinkOAuthFinishRequest{Session: session}, &state); xerr != nil {
		return nil, xerr
	}
	s.mu.Lock()
	delete(s.sessions, session)
	s.mu.Unlock()
	return s.mirror(ctx, l, orgID, userID, &state)
}

// mirror creates the local, credential-free copy of a cloud-managed mailbox.
func (s *service) mirror(ctx context.Context, l *models.CloudLink, orgID, userID uuid.UUID, state *models.PoolLinkMailboxState) (*models.Email, *errx.Error) {
	name := strings.TrimSpace(state.Name)
	if name == "" {
		name = state.Email
	}
	acc, xerr := s.emails.NewManagedAccount(ctx, userID.String(), models.NewOauthAccount{
		OrganizationID: &orgID,
		Provider:       models.InboxProvider(state.Provider),
		Name:           name,
		Email:          state.Email,
	})
	if xerr != nil {
		// Release the cloud side so the next attempt is clean.
		if rerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+state.RemoteID.String(), nil, nil); rerr != nil {
			log.Error().Str("remote_id", state.RemoteID.String()).Str("code", rerr.Identifier).Msg("cloud link: local mirror failed and the cloud link could not be released")
		}
		return nil, xerr
	}
	if _, err := s.repo.Enroll(ctx, acc.ID, state.RemoteID, true); err != nil {
		if s.emailSvc != nil {
			_ = s.emailSvc.Delete(ctx, userID.String(), acc.ID.String())
		}
		// Release the cloud side too: a mailbox left linked to this instance
		// with no mirror here is hidden from the adoptable list and refused on
		// a second attempt, so it could never be recovered from either side.
		if rerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+state.RemoteID.String(), nil, nil); rerr != nil {
			log.Error().Str("remote_id", state.RemoteID.String()).Str("code", rerr.Identifier).Msg("cloud link: local mirror row failed and the cloud link could not be released")
		}
		return nil, errx.InternalError()
	}
	if s.emailSvc != nil {
		if err := s.emailSvc.LoadAccountOntoWorker(ctx, acc.ID); err != nil {
			log.Warn().Err(err).Str("account_id", acc.ID.String()).Msg("cloud link: worker load of managed mailbox failed; reconciler will retry")
		}
	}
	return acc, nil
}

func (s *service) ListWorkspaceMailboxes(ctx context.Context) ([]models.PoolLinkWorkspaceMailbox, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var list []models.PoolLinkWorkspaceMailbox
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/workspace-mailboxes", nil, &list); xerr != nil {
		return nil, xerr
	}
	if list == nil {
		list = []models.PoolLinkWorkspaceMailbox{}
	}
	return list, nil
}

func (s *service) Adopt(ctx context.Context, orgID, userID, cloudAccountID uuid.UUID) (*models.Email, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var state models.PoolLinkMailboxState
	req := models.PoolLinkAdoptRequest{RemoteID: uuid.New(), EmailAccountID: cloudAccountID}
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/mailboxes/adopt", req, &state); xerr != nil {
		return nil, xerr
	}
	return s.mirror(ctx, l, orgID, userID, &state)
}

// AccessToken is the worker's credential for a managed mailbox, cached briefly.
func (s *service) AccessToken(ctx context.Context, accountID uuid.UUID) (*models.PoolLinkAccessToken, *errx.Error) {
	s.mu.Lock()
	if c, ok := s.tokens[accountID]; ok && time.Now().Before(c.expires) {
		s.mu.Unlock()
		return c.token, nil
	}
	s.mu.Unlock()

	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil || !m.Managed {
		return nil, ErrNotManaged
	}
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var tok models.PoolLinkAccessToken
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/mailboxes/"+m.RemoteID.String()+"/token", nil, &tok); xerr != nil {
		return nil, xerr
	}
	// Capped so a cloud-side revocation bites within minutes.
	until := tok.ExpiresAt.Add(-2 * time.Minute)
	if cap := time.Now().Add(tokenCacheMax); until.After(cap) {
		until = cap
	}
	s.mu.Lock()
	s.tokens[accountID] = cachedToken{token: &tok, expires: until}
	s.mu.Unlock()
	return &tok, nil
}

func (s *service) forgetToken(accountID uuid.UUID) {
	s.mu.Lock()
	delete(s.tokens, accountID)
	s.mu.Unlock()
}

// removeManaged deletes the local mirror; the cloud keeps the mailbox in the workspace.
func (s *service) removeManaged(ctx context.Context, userID string, m *models.CloudLinkMailbox) *errx.Error {
	if l, err := s.repo.Get(ctx); err == nil && l != nil {
		if xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+m.RemoteID.String(), nil, nil); xerr != nil && xerr.Identifier != "pool_link_mailbox_not_found" {
			return xerr
		}
	}
	s.forgetToken(m.EmailAccountID)
	if s.emailSvc != nil {
		if xerr := s.emailSvc.Delete(ctx, userID, m.EmailAccountID.String()); xerr != nil && xerr != errx.ErrNotFound {
			return xerr
		}
	}
	return nil
}

// VerifyWarmupToken asks the cloud whether warmup mail in an enrolled mailbox is its own.
func (s *service) VerifyWarmupToken(ctx context.Context, accountID uuid.UUID, token string) (bool, error) {
	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil || m == nil {
		return false, err
	}
	l, xerr := s.link(ctx)
	if xerr != nil {
		return false, xerr
	}
	var out struct {
		Valid bool `json:"valid"`
	}
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/mailboxes/"+m.RemoteID.String()+"/warmup-tokens/"+url.PathEscape(token), nil, &out); xerr != nil {
		return false, xerr
	}
	return out.Valid, nil
}

// IsCloudWarmupDelivery asks the cloud whether a message that arrived without a
// verify header is its own warmup mail. Every send from a Microsoft mailbox
// loses the header in transit, so without this the cloud's warmup would be
// filed as ordinary mail in the owner's inbox.
func (s *service) IsCloudWarmupDelivery(ctx context.Context, accountID uuid.UUID, sender, messageID, subject string) (bool, error) {
	if messageID == "" && (sender == "" || subject == "") {
		return false, nil
	}
	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil || m == nil {
		return false, err
	}
	l, xerr := s.link(ctx)
	if xerr != nil {
		return false, xerr
	}
	var out struct {
		Valid bool `json:"valid"`
	}
	q := models.PoolLinkWarmupDeliveryQuery{Sender: sender, MessageID: messageID, Subject: subject}
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/mailboxes/"+m.RemoteID.String()+"/warmup-deliveries", q, &out); xerr != nil {
		return false, xerr
	}
	return out.Valid, nil
}
