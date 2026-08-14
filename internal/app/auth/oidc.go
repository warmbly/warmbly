package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
)

// oidcStateTTL bounds how long an in-flight authorization may take. Short
// enough that a leaked state is useless, long enough for a real person to type
// a password and approve an MFA prompt at their IdP.
const oidcStateTTL = 10 * time.Minute

// OIDCProvider is the generic OpenID Connect client. Satisfied by
// *oidcauth.Service; an interface here so the auth package does not import it
// and tests can stub it.
type OIDCProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, expectedNonce string) (*idtoken.Claims, error)
	Issuer() string
	ProviderName() string
	DefaultOrgID() string
}

// OIDCRedirect is what the client sends the browser to.
type OIDCRedirect struct {
	URL string `json:"url"`
}

// oidcFlow is the server-side half of one authorization request. Keeping the
// verifier and nonce here, keyed by state, is what makes them single-use: RFC
// 9700 requires PKCE and a one-time state, and an ID token nonce only proves
// anything if the value it is compared against was never reused.
type oidcFlow struct {
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
}

func oidcStateKey(state string) string { return "oidc_state:" + state }

// oidcHandoffTTL is how long the dashboard has to exchange the handoff code for
// the real session. Seconds, not minutes: the redirect and the exchange happen
// back to back.
const oidcHandoffTTL = 60 * time.Second

func oidcHandoffKey(code string) string { return "oidc_handoff:" + code }

// mintHandoff stores a completed login behind a single-use code.
//
// The provider redirects a browser to the callback, so that response cannot be
// JSON: it has to be a redirect the user can follow. Putting tokens in the URL
// would leak them into history, Referer and any proxy log, so the redirect
// carries an opaque code and the dashboard exchanges it over POST.
func (s *authService) mintHandoff(ctx context.Context, result *models.LoginResult) (string, *errx.Error) {
	code, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	payload, err := json.Marshal(result)
	if err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	if err := s.cache.SetEx(ctx, oidcHandoffKey(code), payload, oidcHandoffTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	return code, nil
}

// OIDCExchange swaps a handoff code for the session it stands for. Single use:
// the code is deleted as it is read.
func (s *authService) OIDCExchange(ctx context.Context, code string) (*models.LoginResult, *errx.Error) {
	if code == "" {
		return nil, errx.ErrToken
	}
	raw, err := s.cache.GetDel(ctx, oidcHandoffKey(code)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, errx.ErrToken
	}
	var result models.LoginResult
	if err := json.Unmarshal(raw, &result); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &result, nil
}

// OIDCBegin starts an authorization request.
func (s *authService) OIDCBegin(ctx context.Context) (*OIDCRedirect, *errx.Error) {
	if s.oidc == nil {
		return nil, errx.ErrExternalProvider
	}

	state, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	nonce, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	verifier, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	payload, err := json.Marshal(oidcFlow{Verifier: verifier, Nonce: nonce})
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if err := s.cache.SetEx(ctx, oidcStateKey(state), payload, oidcStateTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &OIDCRedirect{URL: s.oidc.AuthCodeURL(state, nonce, verifier)}, nil
}

// OIDCCallback completes the authorization and returns a single-use handoff
// code the dashboard exchanges for the session.
func (s *authService) OIDCCallback(ctx context.Context, code, state, ipaddr, userAgent string) (string, *errx.Error) {
	if s.oidc == nil {
		return "", errx.ErrExternalProvider
	}
	if code == "" || state == "" {
		return "", errx.ErrExternalCode
	}

	// Consume the state before doing anything with it. A replayed callback
	// finds nothing and is rejected, which is the whole point of one-time
	// state.
	raw, cerr := s.cache.GetDel(ctx, oidcStateKey(state)).Bytes()
	if cerr != nil || len(raw) == 0 {
		return "", errx.ErrExternalCode
	}

	var flow oidcFlow
	if err := json.Unmarshal(raw, &flow); err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}

	claims, err := s.oidc.Exchange(ctx, code, flow.Verifier, flow.Nonce)
	if err != nil {
		// Provider-side failures are user-visible configuration problems more
		// often than attacks, so they are worth reporting rather than burying.
		sentry.CaptureException(err)
		return "", errx.ErrExternalCode
	}

	email, perr := mail.ParseAddress(claims.Email)
	if perr != nil {
		return "", errx.ErrExternalEmail
	}

	userID, rerr := s.resolveFederatedUser(
		ctx,
		models.IdentityProviderOIDC,
		claims.Issuer,
		claims.Subject,
		email,
		claims.GivenName,
		claims.FamilyName,
	)
	if rerr != nil {
		return "", rerr
	}

	result, lerr := s.finishLoginAs(ctx, userID, ipaddr, userAgent, token.AuthProviderEmail)
	if lerr != nil {
		return "", lerr
	}
	return s.mintHandoff(ctx, result)
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
