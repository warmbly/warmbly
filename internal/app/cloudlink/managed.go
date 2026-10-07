package cloudlink

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
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

type cachedToken struct {
	token   *models.PoolLinkAccessToken
	expires time.Time
}

func (s *service) StartOAuth(ctx context.Context, orgID, userID uuid.UUID, provider models.InboxProvider) (*models.CloudLinkOAuthStart, *errx.Error) {
	if !reconciliationLocked(ctx) {
		var result *models.CloudLinkOAuthStart
		xerr := s.reconcileLocked(ctx, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			result, xerr = s.StartOAuth(ctx, orgID, userID, provider)
			return xerr
		})
		return result, xerr
	}
	return s.startManagedOAuth(ctx, orgID, userID, provider)
}

func (s *service) FinishOAuth(ctx context.Context, orgID, userID uuid.UUID, session string) (*models.Email, *errx.Error) {
	if !reconciliationLocked(ctx) {
		var result *models.Email
		xerr := s.reconcileLocked(ctx, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			result, xerr = s.FinishOAuth(ctx, orgID, userID, session)
			return xerr
		})
		return result, xerr
	}
	return s.finishManagedOAuth(ctx, orgID, userID, session)
}

func (s *service) ListWorkspaceMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkWorkspaceMailbox, *errx.Error) {
	l, xerr := s.newLink(ctx, orgID)
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
	if !reconciliationLocked(ctx) {
		var result *models.Email
		xerr := s.reconcileLocked(ctx, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			result, xerr = s.Adopt(ctx, orgID, userID, cloudAccountID)
			return xerr
		})
		return result, xerr
	}
	return s.adoptManagedMailbox(ctx, orgID, userID, cloudAccountID)
}

// AccessToken is the worker's credential for a managed mailbox, cached briefly.
func (s *service) AccessToken(ctx context.Context, accountID uuid.UUID) (*models.PoolLinkAccessToken, *errx.Error) {
	if r, ok := s.repo.(repository.CloudManagedConsentRepository); ok {
		allowed, err := r.CanBrokerManagedToken(ctx, accountID)
		if err != nil {
			return nil, errx.InternalError()
		}
		if !allowed {
			return nil, ErrMailboxInactive
		}
	}
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
	l, xerr := s.mailboxLink(ctx, m)
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
func (s *service) removeManaged(ctx context.Context, orgID string, m *models.CloudLinkMailbox) *errx.Error {
	if err := s.repo.BeginRemoval(ctx, m.EmailAccountID); err != nil {
		return errx.InternalError()
	}
	l, err := s.repo.GetByInstance(ctx, m.InstanceID)
	if err != nil {
		return errx.InternalError()
	}
	if l == nil {
		return ErrNotConnected
	}
	if r, ok := s.repo.(repository.CloudManagedConsentRepository); ok {
		if err := r.RevokeManagedConsents(ctx, l.InstanceID, &m.EmailAccountID); err != nil {
			return errx.InternalError()
		}
	}
	if xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+m.RemoteID.String(), nil, nil); xerr != nil && xerr.Identifier != "pool_link_mailbox_not_found" && !linkAlreadyGone(xerr) {
		return xerr
	}
	s.forgetToken(m.EmailAccountID)
	if s.emailSvc != nil {
		if xerr := s.emailSvc.Delete(ctx, orgID, m.EmailAccountID.String()); xerr != nil && xerr != errx.ErrNotFound {
			return xerr
		}
	} else {
		return errx.InternalError()
	}
	if err := s.repo.Unenroll(ctx, m.EmailAccountID); err != nil {
		return errx.InternalError()
	}
	return nil
}

// VerifyWarmupToken asks the cloud whether warmup mail in an enrolled mailbox is its own.
func (s *service) VerifyWarmupToken(ctx context.Context, accountID uuid.UUID, token string) (bool, error) {
	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil || m == nil {
		return false, err
	}
	l, xerr := s.mailboxLink(ctx, m)
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
	l, xerr := s.mailboxLink(ctx, m)
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

// IsCloudWarmupThreadReply asks the cloud whether a tokenless message answers
// a turn of one of its warmup conversations. The cloud records a yes itself,
// so the turn answering this one is recognised on the next ask.
func (s *service) IsCloudWarmupThreadReply(ctx context.Context, accountID uuid.UUID, messageID string, inReplyTo []string) (bool, error) {
	if len(inReplyTo) == 0 {
		return false, nil
	}
	m, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil || m == nil {
		return false, err
	}
	l, xerr := s.mailboxLink(ctx, m)
	if xerr != nil {
		return false, xerr
	}
	var out struct {
		Valid bool `json:"valid"`
	}
	q := models.PoolLinkWarmupDeliveryQuery{MessageID: messageID, InReplyTo: inReplyTo}
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/mailboxes/"+m.RemoteID.String()+"/warmup-deliveries", q, &out); xerr != nil {
		return false, xerr
	}
	return out.Valid, nil
}
