package connectlink

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/formwire"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// resultTTL is how long a sign-in's outcome waits for the page that started it.
const resultTTL = 15 * time.Minute

func oauthStateKey(state string) string { return "connect_link_oauth:" + state }

// resultKey binds an outcome to its link, so another link's page cannot collect it.
func resultKey(linkID uuid.UUID, key string) string {
	return "connect_link_result:" + linkID.String() + ":" + key
}

// StartOAuth returns the provider's sign-in URL for a client on a live link and the key the page collects the
// outcome with; returnURL is the connect page the browser comes back to.
func (s *Service) StartOAuth(ctx context.Context, link *models.ClientConnectLink, provider models.InboxProvider, returnURL, remoteIP string) (*formwire.ConnectOAuthURL, *errx.Error) {
	if provider != models.InboxProviderGoogle && provider != models.InboxProviderOutlook {
		return nil, ErrProviderNotAllowed
	}
	if xerr := s.admit(ctx, link, provider); xerr != nil {
		return nil, xerr
	}
	if s.states == nil || returnURL == "" {
		return nil, errx.InternalError()
	}
	state, err := newOAuthState()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	key, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	verifier := oauth2.GenerateVerifier()
	authURL, xerr := s.d.Mailboxes.OAuthAuthorizeURL(provider, state, verifier)
	if xerr != nil {
		return nil, xerr
	}
	raw, err := json.Marshal(models.ClientConnectLinkOAuthState{
		LinkID: link.ID, OrganizationID: link.OrganizationID, Provider: string(provider), CodeVerifier: verifier,
		ReturnURL: returnURL, RemoteIPHash: ipHash(link.ID, remoteIP), ExpiresAt: s.d.Now().Add(oauthStateTTL),
		ResultKey: key,
	})
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if err := s.states.Put(ctx, oauthStateKey(state), raw, oauthStateTTL); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &formwire.ConnectOAuthURL{URL: authURL, ResultKey: key}, nil
}

// takeOAuthState consumes a connect link sign-in; a second read of it finds nothing.
func (s *Service) takeOAuthState(ctx context.Context, state string) (*models.ClientConnectLinkOAuthState, *errx.Error) {
	if !IsOAuthState(state) || s.states == nil {
		return nil, ErrSigninExpired
	}
	raw, err := s.states.Take(ctx, oauthStateKey(state))
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			errs.CaptureException(err)
		}
		return nil, ErrSigninExpired
	}
	var out models.ClientConnectLinkOAuthState
	if err := json.Unmarshal(raw, &out); err != nil || out.CodeVerifier == "" || out.LinkID == uuid.Nil ||
		out.OrganizationID == uuid.Nil || !s.d.Now().Before(out.ExpiresAt) {
		return nil, ErrSigninExpired
	}
	return &out, nil
}

// FinishOAuth lands a client's sign-in server-side. The return URL is set whenever the state was ours, so the
// browser can be sent back to the connect page with the outcome, success or not.
func (s *Service) FinishOAuth(ctx context.Context, callbackProvider, code, state, providerErr string) (*Result, string, *errx.Error) {
	sess, xerr := s.takeOAuthState(ctx, strings.TrimSpace(state))
	if xerr != nil {
		return nil, "", xerr
	}
	res, xerr := s.finishOAuth(ctx, sess, callbackProvider, code, providerErr)
	s.leaveResult(ctx, sess, res, xerr)
	return res, sess.ReturnURL, xerr
}

func (s *Service) finishOAuth(ctx context.Context, sess *models.ClientConnectLinkOAuthState, callbackProvider, code, providerErr string) (*Result, *errx.Error) {
	provider := models.InboxProvider(sess.Provider)
	if string(provider) != callbackProvider {
		return nil, ErrSigninExpired
	}
	if providerErr != "" {
		return nil, ErrSigninDeclined
	}
	if strings.TrimSpace(code) == "" {
		return nil, errx.ErrEmailOnboardCode
	}
	link, xerr := s.d.Repo.Get(ctx, sess.OrganizationID, sess.LinkID)
	if xerr != nil {
		return nil, xerr
	}
	if link == nil {
		return nil, ErrRevoked
	}
	if xerr := statusError(link.StatusAt(s.d.Now())); xerr != nil {
		return nil, xerr
	}
	if !link.Allows(provider) {
		return nil, ErrProviderNotAllowed
	}
	if !s.providerAvailable(provider) {
		return nil, ErrProviderUnavailable
	}
	orgID := link.OrganizationID
	return s.connect(ctx, link, provider, sess.RemoteIPHash, func(userID string) (*models.Email, *errx.Error) {
		return s.d.Mailboxes.OAuthConnectWithCode(ctx, userID, &orgID, provider, code, sess.CodeVerifier)
	})
}

// leaveResult stores a sign-in's outcome for the page that started it, which may have lost the sign-in window.
func (s *Service) leaveResult(ctx context.Context, sess *models.ClientConnectLinkOAuthState, res *Result, xerr *errx.Error) {
	if sess.ResultKey == "" {
		return
	}
	out := formwire.ConnectSigninResult{Status: "failed"}
	switch {
	case xerr != nil:
		out.Code, out.Message = xerr.ResponseCode(), xerr.Message
		if xerr.Code == errx.Internal || xerr.Code == errx.ServiceUnavailable {
			out.Message = "Something went wrong on our side, so nothing was connected. Try again in a few minutes."
		}
	case res != nil && res.Account != nil:
		out = formwire.ConnectSigninResult{
			Status: "connected", Email: res.Account.Email, Provider: res.Account.Provider,
			Connected: res.Link.MailboxesConnected, MaxMailboxes: res.Link.MaxMailboxes,
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return
	}
	if err := s.states.Put(ctx, resultKey(sess.LinkID, sess.ResultKey), raw, resultTTL); err != nil {
		errs.CaptureException(err)
	}
}

// SigninResult is the outcome of a sign-in started on link, "pending" until the provider sends the browser back.
func (s *Service) SigninResult(ctx context.Context, link *models.ClientConnectLink, key string) (*formwire.ConnectSigninResult, *errx.Error) {
	pending := &formwire.ConnectSigninResult{Status: "pending"}
	if s.states == nil || key == "" || len(key) > 128 {
		return pending, nil
	}
	raw, err := s.states.Get(ctx, resultKey(link.ID, key))
	if errors.Is(err, redis.Nil) {
		return pending, nil
	}
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	var out formwire.ConnectSigninResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return pending, nil
	}
	return &out, nil
}

// stateStore holds sign-in states server-side; Take and Get return redis.Nil for a key it does not hold.
type stateStore interface {
	Put(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Take(ctx context.Context, key string) ([]byte, error)
	Get(ctx context.Context, key string) ([]byte, error)
}

type redisStates struct{ r *redis.Client }

func (r redisStates) Put(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return r.r.Set(ctx, key, val, ttl).Err()
}

// Take is GetDel, so two callbacks racing on one state cannot both read it.
func (r redisStates) Take(ctx context.Context, key string) ([]byte, error) {
	return r.r.GetDel(ctx, key).Bytes()
}

func (r redisStates) Get(ctx context.Context, key string) ([]byte, error) {
	return r.r.Get(ctx, key).Bytes()
}
