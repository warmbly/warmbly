package cloudlink

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
)

var ErrManagedProtocol = errx.NewWithIdentifier(errx.Conflict, "cloud_link_managed_protocol", "Warmbly Cloud must support durable managed consent before connecting or adopting another mailbox. Existing mailboxes are unchanged.")

func (s *service) managedLink(ctx context.Context, orgID uuid.UUID) (*models.CloudLink, repository.CloudManagedConsentRepository, *errx.Error) {
	l, xerr := s.newLink(ctx, orgID)
	if xerr != nil {
		return nil, nil, xerr
	}
	r, ok := s.repo.(repository.CloudManagedConsentRepository)
	if !ok {
		return nil, nil, errx.InternalError()
	}
	var info models.PoolLinkInstanceInfo
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance", nil, &info); xerr != nil {
		return nil, nil, xerr
	}
	if info.ManagedConsentProtocol != models.ManagedConsentProtocol || info.Instance.ID != l.InstanceID {
		return nil, nil, ErrManagedProtocol
	}
	return l, r, nil
}

func (s *service) startManagedOAuth(ctx context.Context, orgID, userID uuid.UUID, provider models.InboxProvider) (*models.CloudLinkOAuthStart, *errx.Error) {
	if provider != models.InboxProviderGoogle && provider != models.InboxProviderOutlook {
		return nil, errx.ErrEmailOnboardProvider
	}
	l, r, xerr := s.managedLink(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	session, err := crypt.RandomString("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", 43)
	if err != nil {
		return nil, errx.InternalError()
	}
	hash, remoteID, plannedID := crypt.SHA256(session), uuid.New(), uuid.New()
	c := &models.CloudManagedConsent{ID: uuid.New(), OrganizationID: orgID, UserID: &userID, InstanceID: &l.InstanceID,
		RemoteID: &remoteID, SessionHash: &hash, PlannedAccountID: &plannedID, Kind: "oauth", Provider: provider,
		State: "pending", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := r.CreateManagedConsent(ctx, c); err != nil {
		return nil, errx.InternalError()
	}
	req := models.PoolLinkOAuthStartRequest{Protocol: models.ManagedConsentProtocol, RemoteID: remoteID, Session: session,
		Provider: provider, ReturnURL: strings.TrimRight(config.AppBaseURL(), "/") + OAuthReturnPath}
	var res models.PoolLinkOAuthStartResponse
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/oauth/start-correlated", req, &res); xerr != nil {
		return nil, xerr
	}
	if res.Session != session || res.URL == "" {
		return nil, ErrManagedProtocol
	}
	return &models.CloudLinkOAuthStart{URL: res.URL, Session: session}, nil
}

func (s *service) finishManagedOAuth(ctx context.Context, orgID, userID uuid.UUID, session string) (*models.Email, *errx.Error) {
	r, ok := s.repo.(repository.CloudManagedConsentRepository)
	if !ok {
		return nil, errx.InternalError()
	}
	if len(session) < 32 || len(session) > 128 {
		return nil, ErrOAuthSession
	}
	c, err := r.GetManagedConsent(ctx, orgID, userID, crypt.SHA256(session))
	if err != nil {
		return nil, errx.InternalError()
	}
	if c == nil || c.InstanceID == nil || c.Kind != "oauth" || (c.State != "pending" && c.State != "unknown" && c.State != "active") || !time.Now().Before(c.ExpiresAt) {
		return nil, ErrOAuthSession
	}
	l, err := s.repo.GetByInstance(ctx, *c.InstanceID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if l == nil {
		return nil, ErrOAuthSession
	}
	return s.completeManagedConsent(ctx, l, r, c)
}

func (s *service) adoptManagedMailbox(ctx context.Context, orgID, userID, cloudAccountID uuid.UUID) (*models.Email, *errx.Error) {
	l, r, xerr := s.managedLink(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	existing, err := r.FindManagedAdoption(ctx, orgID, userID, l.InstanceID, cloudAccountID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if existing != nil {
		return s.completeManagedConsent(ctx, l, r, existing)
	}
	var list []models.PoolLinkWorkspaceMailbox
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/workspace-mailboxes", nil, &list); xerr != nil {
		return nil, xerr
	}
	var provider models.InboxProvider
	for _, acc := range list {
		if acc.ID == cloudAccountID && acc.Status == "active" {
			provider = models.InboxProvider(acc.Provider)
		}
	}
	if provider != models.InboxProviderGoogle && provider != models.InboxProviderOutlook {
		return nil, ErrOAuthSession
	}
	remoteID, plannedID := uuid.New(), uuid.New()
	c := &models.CloudManagedConsent{ID: uuid.New(), OrganizationID: orgID, UserID: &userID, InstanceID: &l.InstanceID,
		RemoteID: &remoteID, PlannedAccountID: &plannedID, CloudAccountID: &cloudAccountID, Kind: "adopt", Provider: provider,
		State: "pending", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := r.CreateManagedConsent(ctx, c); err != nil {
		return nil, errx.InternalError()
	}
	return s.completeManagedConsent(ctx, l, r, c)
}

func (s *service) completeManagedConsent(ctx context.Context, l *models.CloudLink, r repository.CloudManagedConsentRepository, c *models.CloudManagedConsent) (*models.Email, *errx.Error) {
	if c.InstanceID == nil || *c.InstanceID != l.InstanceID || c.RemoteID == nil || c.PlannedAccountID == nil || c.UserID == nil || l.DisconnectPending ||
		(l.OrganizationID != nil && *l.OrganizationID != c.OrganizationID) ||
		(c.State != "pending" && c.State != "unknown" && c.State != "active") {
		return nil, ErrOAuthSession
	}
	allowed, err := r.ManagedConsentAuthorized(ctx, c)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !allowed {
		return nil, ErrOAuthSession
	}
	var state models.PoolLinkMailboxState
	if c.Kind == "adopt" {
		if c.CloudAccountID == nil {
			return nil, ErrOAuthSession
		}
		req := models.PoolLinkAdoptRequest{Protocol: models.ManagedConsentProtocol, RemoteID: *c.RemoteID, EmailAccountID: *c.CloudAccountID}
		if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/mailboxes/adopt-correlated", req, &state); xerr != nil {
			return nil, xerr
		}
	} else {
		req := models.PoolLinkOAuthFinishRequest{Protocol: models.ManagedConsentProtocol, RemoteID: *c.RemoteID}
		if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/oauth/finish-correlated", req, &state); xerr != nil {
			return nil, xerr
		}
	}
	if state.ConsentCompletedAt == nil || state.ConsentCompletedAt.After(c.ExpiresAt) || state.RemoteID != *c.RemoteID || !state.Managed ||
		state.Status != "active" || state.Provider != string(c.Provider) || state.EmailAccountID == uuid.Nil ||
		(c.CloudAccountID != nil && state.EmailAccountID != *c.CloudAccountID) {
		return nil, ErrOAuthSession
	}
	addr, err := mail.ParseAddress(state.Email)
	if err != nil || addr.Address != state.Email {
		return nil, ErrOAuthSession
	}
	if err := r.BindManagedCloudAccount(ctx, c.OrganizationID, c.ID, state.EmailAccountID); err != nil {
		return nil, errx.InternalError()
	}
	acc, xerr := s.emails.GetByID(ctx, *c.PlannedAccountID)
	if xerr != nil && xerr != errx.ErrNotFound {
		return nil, xerr
	}
	if acc == nil {
		name := strings.TrimSpace(state.Name)
		if name == "" {
			name = state.Email
		}
		acc, xerr = s.emails.NewManagedAccount(ctx, c.UserID.String(), models.NewOauthAccount{ID: *c.PlannedAccountID,
			OrganizationID: &c.OrganizationID, Provider: c.Provider, Name: name, Email: state.Email})
		if xerr != nil {
			return nil, xerr
		}
	}
	if acc.OrganizationID == nil || *acc.OrganizationID != c.OrganizationID || acc.ID != *c.PlannedAccountID || acc.Email != state.Email || acc.Provider != string(c.Provider) || acc.Status != "active" {
		return nil, ErrOAuthSession
	}
	if err := r.SetManagedConsentState(ctx, c.OrganizationID, c.ID, c.State, &acc.ID); err != nil {
		return nil, errx.InternalError()
	}
	if _, err := s.repo.Enroll(ctx, acc.ID, state.RemoteID, l.InstanceID, true); err != nil {
		return nil, errx.InternalError()
	}
	if state.Health == nil || !knownHealthState(state.Health.State) {
		if err := s.repo.InvalidateStanding(ctx, acc.ID); err != nil {
			return nil, errx.InternalError()
		}
	} else if _, ok := s.recordStanding(ctx, acc.ID, state.Health, true); !ok {
		return nil, errx.InternalError()
	}
	if err := r.SetManagedConsentState(ctx, c.OrganizationID, c.ID, "active", &acc.ID); err != nil {
		return nil, errx.InternalError()
	}
	if s.emailSvc != nil {
		if err := s.emailSvc.LoadAccountOntoWorker(ctx, acc.ID); err != nil {
			log.Warn().Str("account_id", acc.ID.String()).Msg("cloud link: managed worker load will retry")
		}
	}
	return acc, nil
}

func (s *service) releaseManagedConsent(ctx context.Context, l *models.CloudLink, r repository.CloudManagedConsentRepository, c *models.CloudManagedConsent) *errx.Error {
	if c.RemoteID == nil {
		return errx.InternalError()
	}
	if err := r.SetManagedConsentState(ctx, c.OrganizationID, c.ID, "pending_remove", c.AccountID); err != nil {
		return errx.InternalError()
	}
	if xerr := s.clientFor(l).do(ctx, http.MethodDelete, "/instance/mailboxes/"+c.RemoteID.String(), nil, nil); xerr != nil &&
		xerr.Identifier != "pool_link_mailbox_not_found" && !linkAlreadyGone(xerr) {
		return xerr
	}
	if c.PlannedAccountID != nil {
		acc, xerr := s.emails.GetByID(ctx, *c.PlannedAccountID)
		if xerr != nil && xerr != errx.ErrNotFound {
			return xerr
		}
		if acc != nil {
			if acc.OrganizationID == nil || *acc.OrganizationID != c.OrganizationID || s.emailSvc == nil {
				return errx.InternalError()
			}
			if xerr := s.emailSvc.Delete(ctx, c.OrganizationID.String(), acc.ID.String()); xerr != nil && xerr != errx.ErrNotFound {
				return xerr
			}
		}
	}
	if err := r.SetManagedConsentState(ctx, c.OrganizationID, c.ID, "revoked", nil); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) reconcileManagedConsents(ctx context.Context, l *models.CloudLink) *errx.Error {
	r, ok := s.repo.(repository.CloudManagedConsentRepository)
	if !ok {
		return nil
	}
	rows, err := r.ListManagedConsents(ctx, l.InstanceID)
	if err != nil {
		return errx.InternalError()
	}
	for i := range rows {
		c := &rows[i]
		allowed, err := r.ManagedConsentAuthorized(ctx, c)
		if err != nil {
			return errx.InternalError()
		}
		if c.State == "pending_remove" || l.DisconnectPending || !allowed {
			if xerr := s.releaseManagedConsent(ctx, l, r, c); xerr != nil {
				return xerr
			}
			continue
		}
		if _, xerr := s.completeManagedConsent(ctx, l, r, c); xerr != nil {
			if !time.Now().Before(c.ExpiresAt) || xerr.Identifier == "pool_link_oauth_session" || linkAlreadyGone(xerr) {
				if xerr := s.releaseManagedConsent(ctx, l, r, c); xerr != nil {
					return xerr
				}
			} else if xerr.Identifier == "pool_link_oauth_pending" {
				continue
			} else if xerr.Identifier == "pool_link_oauth_unknown" {
				if err := r.SetManagedConsentState(ctx, c.OrganizationID, c.ID, "unknown", nil); err != nil {
					return errx.InternalError()
				}
			} else {
				return xerr
			}
		}
	}
	return nil
}
