package poollink

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

// Cloud-managed mailboxes: the consent runs on this deployment's OAuth app,
// the grant stays here, and the instance sends with brokered access tokens.

var (
	ErrOAuthReturnURL    = errx.NewWithIdentifier(errx.BadRequest, "pool_link_return_url", "The return URL must be an http(s) URL on the address this instance registered when it was linked.")
	ErrOAuthInstanceURL  = errx.NewWithIdentifier(errx.Unprocessable, "pool_link_instance_url", "This instance was linked without its own address, so Warmbly Cloud has nowhere to send a sign-in back to. Set APP_URL on the instance, then disconnect it and link it again.")
	ErrOAuthPending      = errx.NewWithIdentifier(errx.Conflict, "pool_link_oauth_pending", "The sign-in has not completed yet.")
	ErrOAuthSession      = errx.NewWithIdentifier(errx.NotFound, "pool_link_oauth_session", "That sign-in session is unknown or has expired. Start again.")
	ErrOAuthBrowser      = errx.NewWithIdentifier(errx.BadRequest, "pool_link_oauth_browser", "The sign-in was not continued from the Warmbly Cloud page that started it, in this browser. Start again from your instance.")
	ErrMailboxNotManaged = errx.NewWithIdentifier(errx.Forbidden, "pool_link_not_managed", "This mailbox's credential is held by the instance, not by Warmbly Cloud.")
	ErrMailboxBlocked    = errx.NewWithIdentifier(errx.Forbidden, "pool_link_mailbox_blocked", "Warmbly Cloud has blocked this mailbox for hurting the pool. Sending from it is suspended until it is reviewed.")
	ErrMailboxInactive   = errx.NewWithIdentifier(errx.Forbidden, "pool_link_mailbox_inactive", "This mailbox is not active on Warmbly Cloud. Reconnect it to keep sending.")
	ErrAlreadyAdopted    = errx.NewWithIdentifier(errx.Conflict, "pool_link_already_adopted", "That mailbox is already linked to an instance.")
	ErrNotAdoptable      = errx.NewWithIdentifier(errx.Unprocessable, "pool_link_not_adoptable", "Only active Google and Microsoft mailboxes in this workspace can be linked.")
)

// BrokerStatePrefix marks a consent state as brokered so the callback can route it.
const BrokerStatePrefix = "pl_"

const CorrelatedBrokerStatePrefix = "pc1_"

func IsBrokerState(state string) bool {
	return strings.HasPrefix(state, BrokerStatePrefix) || strings.HasPrefix(state, CorrelatedBrokerStatePrefix)
}

// BrokerConsentPath is the cloud page that names the requesting instance before the provider opens.
const BrokerConsentPath = "/addresses/connect"

const brokerTTL = 10 * time.Minute

type brokerState struct {
	RemoteID   uuid.UUID `json:"remote_id,omitempty"`
	InstanceID uuid.UUID `json:"instance_id"`
	Provider   string    `json:"provider"`
	ReturnURL  string    `json:"return_url"`
	Session    string    `json:"session"`
	// Verifier is the PKCE verifier; it never leaves the cloud.
	Verifier string `json:"verifier"`
	// Binding is the browser cookie set by the consent page; the callback requires it.
	Binding string `json:"binding,omitempty"`
}

type brokerResult struct {
	StartURL   string    `json:"start_url,omitempty"`
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

// instanceOrigin parses the address an instance registered when it was linked.
func instanceOrigin(instanceURL string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(instanceURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, false
	}
	return u, true
}

// returnURLAllowed: same scheme and host as the instance's registered address, never anywhere else.
func returnURLAllowed(raw, instanceURL string) bool {
	iu, ok := instanceOrigin(instanceURL)
	if !ok {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, iu.Scheme) || !strings.EqualFold(u.Host, iu.Host) {
		return false
	}
	return true
}

func (s *service) StartOAuth(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkOAuthStartRequest) (*models.PoolLinkOAuthStartResponse, *errx.Error) {
	if inst.RemoteOrganizationID == nil {
		return nil, ErrLegacyLink
	}
	if req.Protocol != 0 {
		if req.Protocol != models.ManagedConsentProtocol || req.RemoteID == uuid.Nil || len(req.Session) < 32 || len(req.Session) > 128 {
			return nil, ErrBadRequest
		}
		if !managedOperationLocked(ctx) {
			var out *models.PoolLinkOAuthStartResponse
			xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
				var xerr *errx.Error
				out, xerr = s.StartOAuth(ctx, inst, req)
				return xerr
			})
			return out, xerr
		}
		if xerr := s.currentManagedInstance(ctx, inst); xerr != nil {
			return nil, xerr
		}
	}
	if s.cache == nil {
		return nil, errx.InternalError()
	}
	if req.Provider != models.InboxProviderGoogle && req.Provider != models.InboxProviderOutlook {
		return nil, errx.ErrEmailOnboardProvider
	}
	// The cloud's own Google app decides here, not the linked instance's.
	if req.Provider == models.InboxProviderGoogle && !config.GoogleOAuthConnect() {
		return nil, errx.ErrEmailOnboardGoogleOAuthDisabled
	}
	if _, ok := instanceOrigin(inst.URL); !ok {
		return nil, ErrOAuthInstanceURL
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
	origin, xerr := s.emailSvc.OAuthCallbackOrigin(req.Provider)
	if xerr != nil {
		return nil, xerr
	}
	ttl := brokerTTL
	if req.Protocol != 0 {
		r := s.repo.(repository.PoolLinkManagedRepository)
		hash, planned := crypt.SHA256(req.Session), uuid.New()
		op := &models.PoolLinkManagedOperation{OrganizationID: inst.OrganizationID, InstanceID: &inst.ID, RemoteID: &req.RemoteID,
			SessionHash: &hash, Kind: "oauth", Provider: req.Provider, PlannedAccountID: &planned, ExpiresAt: time.Now().Add(brokerTTL)}
		if err := r.CreateManagedOperation(ctx, op); err != nil {
			return nil, errx.InternalError()
		}
		op, err := r.GetManagedOperation(ctx, inst.ID, req.RemoteID)
		if err != nil {
			return nil, errx.InternalError()
		}
		if op == nil || op.State != "pending" || !time.Now().Before(op.ExpiresAt) {
			return nil, ErrOAuthSession
		}
		ttl = time.Until(op.ExpiresAt)
		var previous brokerResult
		if err := s.cache.GetJSON(ctx, brokerSessionKey(req.Session), &previous); err == nil {
			if previous.InstanceID != inst.ID || !previous.Pending || previous.StartURL == "" {
				return nil, ErrOAuthSession
			}
			return &models.PoolLinkOAuthStartResponse{URL: previous.StartURL, Session: req.Session}, nil
		} else if !errors.Is(err, redis.Nil) {
			return nil, errx.InternalError()
		}
	}
	nonce, err := crypt.Nonce()
	if err != nil {
		return nil, errx.InternalError()
	}
	session, err := crypt.Nonce()
	if err != nil {
		return nil, errx.InternalError()
	}
	if req.Protocol != 0 {
		session = req.Session
	}
	state := BrokerStatePrefix + nonce
	remoteID := uuid.Nil
	if req.Protocol != 0 {
		state = CorrelatedBrokerStatePrefix + nonce
		remoteID = req.RemoteID
	}
	st := brokerState{RemoteID: remoteID, InstanceID: inst.ID, Provider: string(req.Provider), ReturnURL: req.ReturnURL, Session: session, Verifier: oauth2.GenerateVerifier()}
	consentURL := origin + BrokerConsentPath + "?" + url.Values{"state": {state}}.Encode()
	if err := s.cache.SetJSON(ctx, brokerStateKey(state), st, ttl); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.cache.SetJSON(ctx, brokerSessionKey(session), brokerResult{InstanceID: inst.ID, Pending: true, StartURL: consentURL}, ttl); err != nil {
		return nil, errx.InternalError()
	}
	// Only the consent page hands out the provider URL, to the browser that continues there.
	return &models.PoolLinkOAuthStartResponse{URL: consentURL, Session: session}, nil
}

func (s *service) peekBrokerState(ctx context.Context, state string) (*brokerState, *errx.Error) {
	if s.cache == nil || !IsBrokerState(state) {
		return nil, ErrOAuthSession
	}
	var st brokerState
	if err := s.cache.GetJSON(ctx, brokerStateKey(state), &st); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrOAuthSession
		}
		return nil, errx.InternalError()
	}
	return &st, nil
}

// DescribeOAuthConsent names who is asking, for the page shown before the provider opens.
func (s *service) DescribeOAuthConsent(ctx context.Context, state string) (*models.PoolLinkOAuthConsent, *errx.Error) {
	st, xerr := s.peekBrokerState(ctx, state)
	if xerr != nil {
		return nil, xerr
	}
	inst, err := s.repo.GetInstance(ctx, st.InstanceID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if inst == nil || inst.RevokedAt != nil {
		return nil, ErrInstanceRevoked
	}
	iu, ok := instanceOrigin(inst.URL)
	if !ok {
		return nil, ErrOAuthInstanceURL
	}
	out := &models.PoolLinkOAuthConsent{
		Provider:     models.InboxProvider(st.Provider),
		InstanceName: displayname.Displayable(inst.Name),
		InstanceHost: iu.Host,
	}
	if org, xerr := s.orgs.Get(ctx, inst.OrganizationID); xerr == nil && org != nil {
		out.WorkspaceName = displayname.Displayable(org.Name)
	}
	return out, nil
}

// ContinueOAuth binds the round trip to the browser that chose to continue and returns the provider URL.
func (s *service) ContinueOAuth(ctx context.Context, state, binding string) (string, *errx.Error) {
	pending, xerr := s.peekBrokerState(ctx, state)
	if xerr != nil {
		return "", xerr
	}
	if pending.RemoteID != uuid.Nil && !managedOperationLocked(ctx) {
		var out string
		xerr := s.withManagedOperationLock(ctx, pending.InstanceID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.ContinueOAuth(ctx, state, binding)
			return xerr
		})
		return out, xerr
	}
	if len(binding) < 16 {
		return "", ErrOAuthBrowser
	}
	st, xerr := s.peekBrokerState(ctx, state)
	if xerr != nil {
		return "", xerr
	}
	if st.RemoteID != uuid.Nil {
		inst, err := s.repo.GetInstance(ctx, st.InstanceID)
		if err != nil {
			return "", errx.InternalError()
		}
		if inst == nil {
			return "", ErrInstanceRevoked
		}
		if xerr := s.currentManagedInstance(ctx, inst); xerr != nil {
			return "", xerr
		}
		op, err := s.repo.(repository.PoolLinkManagedRepository).GetManagedOperation(ctx, st.InstanceID, st.RemoteID)
		if err != nil {
			return "", errx.InternalError()
		}
		if op == nil || op.State != "pending" || !time.Now().Before(op.ExpiresAt) {
			return "", ErrOAuthSession
		}
	}
	st.Binding = binding
	raw, err := json.Marshal(st)
	if err != nil {
		return "", errx.InternalError()
	}
	// XX with KEEPTTL never revives a state the callback already consumed.
	if err := s.cache.SetArgs(ctx, brokerStateKey(state), raw, redis.SetArgs{Mode: "XX", KeepTTL: true}).Err(); err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrOAuthSession
		}
		return "", errx.InternalError()
	}
	return s.emailSvc.OAuthAuthorizeURL(models.InboxProvider(st.Provider), state, st.Verifier)
}

// CompleteOAuthCallback finishes a brokered consent; every completed outcome redirects to the instance.
func (s *service) CompleteOAuthCallback(ctx context.Context, provider, code, state, providerErr, binding string) (string, *errx.Error) {
	pendingState, xerr := s.peekBrokerState(ctx, state)
	if xerr != nil {
		return "", xerr
	}
	if pendingState.RemoteID != uuid.Nil {
		if pendingState.Provider != provider || !bindingMatches(pendingState.Binding, binding) {
			return "", ErrOAuthBrowser
		}
		if !managedOperationLocked(ctx) {
			var out string
			xerr := s.withManagedOperationLock(ctx, pendingState.InstanceID, func(ctx context.Context) *errx.Error {
				var xerr *errx.Error
				out, xerr = s.CompleteOAuthCallback(ctx, provider, code, state, providerErr, binding)
				return xerr
			})
			return out, xerr
		}
	}
	if s.cache == nil {
		return "", ErrOAuthSession
	}
	raw, err := s.cache.GetDel(ctx, brokerStateKey(state)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrOAuthSession
		}
		return "", errx.InternalError()
	}
	var st brokerState
	if err := json.Unmarshal(raw, &st); err != nil || st.Provider != provider || st.Verifier == "" {
		return "", ErrOAuthSession
	}
	if !bindingMatches(st.Binding, binding) {
		s.storeBrokerResult(ctx, st.Session, brokerResult{InstanceID: st.InstanceID, ErrorCode: ErrOAuthBrowser.Identifier, ErrorText: ErrOAuthBrowser.Message})
		return "", ErrOAuthBrowser
	}
	res := brokerResult{InstanceID: st.InstanceID}
	if providerErr != "" {
		if st.RemoteID != uuid.Nil {
			if err := s.repo.(repository.PoolLinkManagedRepository).RevokeManagedOperations(ctx, st.InstanceID, &st.RemoteID); err != nil {
				return "", errx.InternalError()
			}
		}
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
	s.storeBrokerResult(ctx, st.Session, res)
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
	return st.ReturnURL + sep + q.Encode(), nil
}

func bindingMatches(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func (s *service) storeBrokerResult(ctx context.Context, session string, res brokerResult) {
	if err := s.cache.SetJSON(ctx, brokerSessionKey(session), res, brokerTTL); err != nil {
		log.Error().Err(err).Msg("pool link: could not store brokered consent result")
	}
}

func (s *service) connectBrokered(ctx context.Context, st brokerState, code string) (uuid.UUID, *errx.Error) {
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var remoteID uuid.UUID
		xerr := s.withManagedOperationLock(ctx, st.InstanceID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			remoteID, xerr = s.connectBrokered(ctx, st, code)
			return xerr
		})
		return remoteID, xerr
	}
	inst, err := s.repo.GetInstance(ctx, st.InstanceID)
	if err != nil {
		return uuid.Nil, errx.InternalError()
	}
	if inst == nil || inst.RevokedAt != nil {
		return uuid.Nil, ErrInstanceRevoked
	}
	if inst.RemoteOrganizationID == nil {
		return uuid.Nil, ErrLegacyLink
	}
	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr != nil {
		return uuid.Nil, xerr
	}
	orgID := inst.OrganizationID
	if st.RemoteID != uuid.Nil {
		r := s.repo.(repository.PoolLinkManagedRepository)
		op, err := r.GetManagedOperation(ctx, inst.ID, st.RemoteID)
		if err != nil {
			return uuid.Nil, errx.InternalError()
		}
		if op == nil || op.SessionHash == nil || *op.SessionHash != crypt.SHA256(st.Session) || op.Provider != models.InboxProvider(st.Provider) || op.PlannedAccountID == nil {
			return uuid.Nil, ErrOAuthSession
		}
		claimed, err := r.ClaimManagedOperation(ctx, inst.ID, st.RemoteID)
		if err != nil {
			return uuid.Nil, errx.InternalError()
		}
		if !claimed {
			return uuid.Nil, ErrOAuthSession
		}
		if xerr := s.managedCapacity(ctx, inst, st.RemoteID); xerr != nil {
			return uuid.Nil, xerr
		}
		acc, xerr := s.emailSvc.OAuthConnectWithCodeForAccount(ctx, userID, &orgID, models.InboxProvider(st.Provider), code, st.Verifier, *op.PlannedAccountID)
		if xerr != nil {
			return uuid.Nil, ErrOAuthUnknown
		}
		if err := r.CompleteManagedOperation(ctx, inst.ID, st.RemoteID, acc.ID); err != nil {
			return uuid.Nil, errx.InternalError()
		}
		if _, xerr := s.FinishManagedOAuth(ctx, inst, models.PoolLinkOAuthFinishRequest{Protocol: models.ManagedConsentProtocol, RemoteID: st.RemoteID}); xerr != nil {
			return uuid.Nil, xerr
		}
		return st.RemoteID, nil
	}
	acc, xerr := s.emailSvc.OAuthConnectWithCode(ctx, userID, &orgID, models.InboxProvider(st.Provider), code, st.Verifier)
	if xerr != nil {
		return uuid.Nil, xerr
	}
	remoteID := uuid.New()
	if err := s.repo.EnrollMailbox(ctx, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: remoteID, EmailAccountID: acc.ID, Managed: true}); err != nil {
		_ = s.emailSvc.Delete(ctx, orgID.String(), acc.ID.String())
		return uuid.Nil, errx.InternalError()
	}
	s.startWarmup(ctx, orgID.String(), acc.ID)
	return remoteID, nil
}

// startWarmup: failures here are retried by the reconciler.
func (s *service) startWarmup(ctx context.Context, orgID string, accountID uuid.UUID) {
	if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, orgID, accountID.String(), "start"); xerr != nil {
		log.Warn().Str("account_id", accountID.String()).Str("code", xerr.Identifier).Str("error", xerr.Message).Msg("pool link: warmup start failed after enrollment")
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
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok {
		if !managedOperationLocked(ctx) {
			var out *models.PoolLinkAccessToken
			xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
				var xerr *errx.Error
				out, xerr = s.AccessToken(ctx, inst, remoteID)
				return xerr
			})
			return out, xerr
		}
		if _, xerr := s.managedAuthority(ctx, inst, remoteID); xerr != nil {
			return nil, xerr
		}
	}
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
	if inst.RemoteOrganizationID == nil {
		return nil, ErrLegacyLink
	}
	list, err := s.repo.ListAdoptableMailboxes(ctx, inst.OrganizationID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return list, nil
}

// Adopt links a mailbox that was connected directly on the workspace.
func (s *service) Adopt(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkAdoptRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if inst.RemoteOrganizationID == nil {
		return nil, ErrLegacyLink
	}
	if req.Protocol != 0 {
		if req.Protocol != models.ManagedConsentProtocol || req.RemoteID == uuid.Nil || req.EmailAccountID == uuid.Nil {
			return nil, ErrBadRequest
		}
		return s.adoptManaged(ctx, inst, req)
	}
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.Adopt(ctx, inst, req)
			return xerr
		})
		return out, xerr
	}
	if req.RemoteID == uuid.Nil || req.EmailAccountID == uuid.Nil {
		return nil, ErrBadRequest
	}
	acc, xerr := s.emails.GetByID(ctx, req.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	// A mailbox under an administrator's grant has no token of its own to broker.
	if acc.OrganizationID == nil || *acc.OrganizationID != inst.OrganizationID || acc.Status != "active" ||
		acc.AuthMethod == models.MailAuthDelegated ||
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
	s.startWarmup(ctx, inst.OrganizationID.String(), acc.ID)
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
	return t != nil && (t.RecipientAccountID == m.EmailAccountID || t.SenderAccountID == m.EmailAccountID), nil
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
	// A reply typed by hand in a warmup thread has no token and no known id of
	// its own; it is warmup by what it answers. Recorded so the turn answering
	// it is recognised too, exactly as the cloud's own consumer would.
	if len(q.InReplyTo) > 0 {
		reply, err := s.warmup.IsWarmupThreadReply(ctx, m.EmailAccountID, q.InReplyTo)
		if err != nil {
			return false, errx.InternalError()
		}
		if reply {
			if err := s.warmup.RecordWarmupThreadMessage(ctx, m.EmailAccountID, q.MessageID); err != nil {
				return false, errx.InternalError()
			}
			return true, nil
		}
	}
	ok, err := s.warmup.IsWarmupDelivery(ctx, m.EmailAccountID, q.Sender, q.MessageID, q.Subject)
	if errors.Is(err, repository.ErrWarmupDeliveryPending) {
		return false, errx.ErrServiceDown
	}
	if err != nil {
		return false, errx.InternalError()
	}
	return ok, nil
}
