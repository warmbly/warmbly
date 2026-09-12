package poollink

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// Cloud-managed mailboxes: the consent runs on this deployment's OAuth app,
// the grant stays here, and the instance sends with brokered access tokens.

var (
	ErrOAuthReturnURL    = errx.NewWithIdentifier(errx.BadRequest, "pool_link_return_url", "The return URL must be an absolute http(s) URL on the linked instance.")
	ErrOAuthPending      = errx.NewWithIdentifier(errx.Conflict, "pool_link_oauth_pending", "The sign-in has not completed yet.")
	ErrOAuthSession      = errx.NewWithIdentifier(errx.NotFound, "pool_link_oauth_session", "That sign-in session is unknown or has expired. Start again.")
	ErrMailboxNotManaged = errx.NewWithIdentifier(errx.Forbidden, "pool_link_not_managed", "This mailbox's credential is held by the instance, not by Warmbly Cloud.")
	ErrMailboxBlocked    = errx.NewWithIdentifier(errx.Forbidden, "pool_link_mailbox_blocked", "Warmbly Cloud has blocked this mailbox for hurting the pool. Sending from it is suspended until it is reviewed.")
	ErrMailboxInactive   = errx.NewWithIdentifier(errx.Forbidden, "pool_link_mailbox_inactive", "This mailbox is not active on Warmbly Cloud. Reconnect it to keep sending.")
	ErrAlreadyAdopted    = errx.NewWithIdentifier(errx.Conflict, "pool_link_already_adopted", "That mailbox is already linked to an instance.")
	ErrNotAdoptable      = errx.NewWithIdentifier(errx.Unprocessable, "pool_link_not_adoptable", "Only active Google and Microsoft mailboxes in this workspace can be linked.")
)

// BrokerStatePrefix marks a consent state as brokered so the callback can route it.
const BrokerStatePrefix = "pl_"

const brokerTTL = 10 * time.Minute

type brokerState struct {
	InstanceID uuid.UUID `json:"instance_id"`
	Provider   string    `json:"provider"`
	ReturnURL  string    `json:"return_url"`
	Session    string    `json:"session"`
}

type brokerResult struct {
	InstanceID uuid.UUID `json:"instance_id"`
	RemoteID   uuid.UUID `json:"remote_id"`
	Pending    bool      `json:"pending"`
	ErrorCode  string    `json:"error_code,omitempty"`
	ErrorText  string    `json:"error_text,omitempty"`
}

// CacheWirer is how main attaches Redis, which holds consent state for the round trip.
type CacheWirer interface{ WireCache(*cache.Cache) }

func (s *service) WireCache(c *cache.Cache) { s.cache = c }

func brokerStateKey(state string) string     { return "poollink:oauth:state:" + state }
func brokerSessionKey(session string) string { return "poollink:oauth:session:" + session }

// returnURLAllowed: absolute http(s), and on the instance's own host when one is known.
func returnURLAllowed(raw, instanceURL string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	if iu, err := url.Parse(instanceURL); err == nil && iu.Host != "" && !strings.EqualFold(iu.Host, u.Host) {
		return false
	}
	return true
}

func (s *service) StartOAuth(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkOAuthStartRequest) (*models.PoolLinkOAuthStartResponse, *errx.Error) {
	if s.cache == nil {
		return nil, errx.InternalError()
	}
	if req.Provider != models.InboxProviderGoogle && req.Provider != models.InboxProviderOutlook {
		return nil, errx.ErrEmailOnboardProvider
	}
	if !returnURLAllowed(req.ReturnURL, inst.URL) {
		return nil, ErrOAuthReturnURL
	}
	plan, xerr := s.Plan(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	if plan.MailboxLimit != nil && plan.Enrolled >= *plan.MailboxLimit {
		return nil, ErrMailboxLimit
	}
	nonce, err := crypt.Nonce()
	if err != nil {
		return nil, errx.InternalError()
	}
	session, err := crypt.Nonce()
	if err != nil {
		return nil, errx.InternalError()
	}
	state := BrokerStatePrefix + nonce
	authURL, xerr := s.emailSvc.OAuthAuthorizeURL(req.Provider, state)
	if xerr != nil {
		return nil, xerr
	}
	st := brokerState{InstanceID: inst.ID, Provider: string(req.Provider), ReturnURL: req.ReturnURL, Session: session}
	if err := s.cache.SetJSON(ctx, brokerStateKey(state), st, brokerTTL); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.cache.SetJSON(ctx, brokerSessionKey(session), brokerResult{InstanceID: inst.ID, Pending: true}, brokerTTL); err != nil {
		return nil, errx.InternalError()
	}
	return &models.PoolLinkOAuthStartResponse{URL: authURL, Session: session}, nil
}

// CompleteOAuthCallback finishes a brokered consent; every outcome redirects to the instance.
func (s *service) CompleteOAuthCallback(ctx context.Context, provider, code, state, providerErr string) string {
	if s.cache == nil {
		return ""
	}
	var st brokerState
	if err := s.cache.GetJSON(ctx, brokerStateKey(state), &st); err != nil {
		return ""
	}
	_ = s.cache.Del(ctx, brokerStateKey(state)).Err()
	if st.Provider != provider {
		return ""
	}
	res := brokerResult{InstanceID: st.InstanceID}
	if providerErr != "" {
		res.ErrorCode, res.ErrorText = providerErr, "The provider did not complete the sign-in."
	} else if code == "" {
		res.ErrorCode, res.ErrorText = "missing_code", "The provider returned no authorization code."
	} else if remoteID, xerr := s.connectBrokered(ctx, st, code); xerr != nil {
		res.ErrorCode, res.ErrorText = xerr.Identifier, xerr.Message
		if res.ErrorCode == "" {
			res.ErrorCode = "pool_link_oauth_failed"
		}
	} else {
		res.RemoteID = remoteID
	}
	if err := s.cache.SetJSON(ctx, brokerSessionKey(st.Session), res, brokerTTL); err != nil {
		log.Error().Err(err).Msg("pool link: could not store brokered consent result")
	}
	q := url.Values{"session": {st.Session}}
	if res.ErrorCode != "" {
		q.Set("status", "error")
		q.Set("error", res.ErrorCode)
		q.Set("message", res.ErrorText)
	} else {
		q.Set("status", "ok")
	}
	sep := "?"
	if strings.Contains(st.ReturnURL, "?") {
		sep = "&"
	}
	return st.ReturnURL + sep + q.Encode()
}

func (s *service) connectBrokered(ctx context.Context, st brokerState, code string) (uuid.UUID, *errx.Error) {
	inst, err := s.repo.GetInstance(ctx, st.InstanceID)
	if err != nil {
		return uuid.Nil, errx.InternalError()
	}
	if inst == nil || inst.RevokedAt != nil {
		return uuid.Nil, ErrInstanceRevoked
	}
	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr != nil {
		return uuid.Nil, xerr
	}
	orgID := inst.OrganizationID
	acc, xerr := s.emailSvc.OAuthConnectWithCode(ctx, userID, &orgID, models.InboxProvider(st.Provider), code)
	if xerr != nil {
		return uuid.Nil, xerr
	}
	remoteID := uuid.New()
	if err := s.repo.EnrollMailbox(ctx, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: remoteID, EmailAccountID: acc.ID, Managed: true}); err != nil {
		_ = s.emailSvc.Delete(ctx, userID, acc.ID.String())
		return uuid.Nil, errx.InternalError()
	}
	s.startWarmup(ctx, userID, acc.ID)
	return remoteID, nil
}

// startWarmup: failures here are retried by the reconciler.
func (s *service) startWarmup(ctx context.Context, userID string, accountID uuid.UUID) {
	if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, userID, accountID.String(), "start"); xerr != nil {
		log.Warn().Str("account_id", accountID.String()).Msg("pool link: warmup start failed after enrollment")
	}
	if err := s.emailSvc.LoadAccountOntoWorker(ctx, accountID); err != nil {
		log.Warn().Err(err).Str("account_id", accountID.String()).Msg("pool link: worker load failed; reconciler will retry")
	}
	if s.scheduler != nil {
		_ = s.scheduler.EnsureWarmupScheduled(ctx, accountID)
	}
}

func (s *service) FinishOAuth(ctx context.Context, inst *models.PoolLinkInstance, session string) (*models.PoolLinkMailboxState, *errx.Error) {
	if s.cache == nil || strings.TrimSpace(session) == "" {
		return nil, ErrOAuthSession
	}
	var res brokerResult
	if err := s.cache.GetJSON(ctx, brokerSessionKey(session), &res); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrOAuthSession
		}
		return nil, errx.InternalError()
	}
	if res.InstanceID != inst.ID {
		return nil, ErrOAuthSession
	}
	if res.Pending {
		return nil, ErrOAuthPending
	}
	_ = s.cache.Del(ctx, brokerSessionKey(session)).Err()
	if res.ErrorCode != "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, res.ErrorCode, res.ErrorText)
	}
	return s.GetMailbox(ctx, inst, res.RemoteID)
}

// AccessToken is the enforcement point: a revoked link or a removed, inactive or blocked mailbox gets no token.
func (s *service) AccessToken(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkAccessToken, *errx.Error) {
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil {
		return nil, ErrMailboxNotFound
	}
	if !m.Managed {
		return nil, ErrMailboxNotManaged
	}
	acc, xerr := s.emails.GetByID(ctx, m.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc.Status != "active" {
		return nil, ErrMailboxInactive
	}
	if s.analytics != nil {
		if status, xerr := s.analytics.GetAccountStatus(ctx, inst.OrganizationID, acc.ID); xerr == nil && status != nil && status.WarmupHealth != nil && status.WarmupHealth.State == "blocked" {
			return nil, ErrMailboxBlocked
		}
	}
	tok, xerr := s.emailSvc.OAuthAccessToken(ctx, acc.ID)
	if xerr != nil {
		return nil, xerr
	}
	_ = s.repo.TouchMailboxToken(ctx, inst.ID, remoteID)
	return &models.PoolLinkAccessToken{AccessToken: tok.AccessToken, ExpiresAt: tok.Expiry, Provider: acc.Provider, Email: acc.Email}, nil
}

func (s *service) ListWorkspaceMailboxes(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkWorkspaceMailbox, *errx.Error) {
	list, err := s.repo.ListAdoptableMailboxes(ctx, inst.OrganizationID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return list, nil
}

// Adopt links a mailbox that was connected directly on the workspace.
func (s *service) Adopt(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkAdoptRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if req.RemoteID == uuid.Nil || req.EmailAccountID == uuid.Nil {
		return nil, ErrBadRequest
	}
	acc, xerr := s.emails.GetByID(ctx, req.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc.OrganizationID == nil || *acc.OrganizationID != inst.OrganizationID || acc.Status != "active" ||
		(acc.Provider != string(models.InboxProviderGoogle) && acc.Provider != string(models.InboxProviderOutlook)) {
		return nil, ErrNotAdoptable
	}
	if existing, err := s.repo.GetMailboxByAccount(ctx, acc.ID); err != nil {
		return nil, errx.InternalError()
	} else if existing != nil {
		return nil, ErrAlreadyAdopted
	}
	if err := s.repo.EnrollMailbox(ctx, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: req.RemoteID, EmailAccountID: acc.ID, Managed: true}); err != nil {
		return nil, errx.InternalError()
	}
	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr == nil {
		s.startWarmup(ctx, userID, acc.ID)
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

// VerifyWarmupToken lets a linked instance tell the cloud's warmup mail apart
// from anything else arriving in a mailbox it warms.
func (s *service) VerifyWarmupToken(ctx context.Context, inst *models.PoolLinkInstance, remoteID, token uuid.UUID) (bool, *errx.Error) {
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return false, errx.InternalError()
	}
	if m == nil {
		return false, ErrMailboxNotFound
	}
	if s.warmup == nil {
		return false, nil
	}
	t, err := s.warmup.FindWarmupToken(ctx, token)
	if err != nil {
		return false, errx.InternalError()
	}
	return t != nil && t.RecipientAccountID == m.EmailAccountID, nil
}

// VerifyWarmupDelivery answers for warmup mail whose verify header did not
// survive delivery, which is every send from a Microsoft mailbox.
func (s *service) VerifyWarmupDelivery(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, q models.PoolLinkWarmupDeliveryQuery) (bool, *errx.Error) {
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return false, errx.InternalError()
	}
	if m == nil {
		return false, ErrMailboxNotFound
	}
	if s.warmup == nil {
		return false, nil
	}
	ok, err := s.warmup.IsWarmupDelivery(ctx, m.EmailAccountID, q.Sender, q.MessageID, q.Subject)
	if err != nil {
		return false, errx.InternalError()
	}
	return ok, nil
}
