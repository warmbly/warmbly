package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
)

// ssoStateTTL bounds an in-flight authorization: long enough to type a password
// and approve an MFA prompt at the provider, short enough that a leaked state
// is useless.
const ssoStateTTL = 10 * time.Minute

// FederatedProvider is one browser sign-in flow. All three hand back the same
// verified claims, so the service runs one login path rather than three.
// Satisfied by *oidcauth.Service, *socialauth.Google and *socialauth.Apple; an
// interface so the auth package imports none of them and tests can stub it.
type FederatedProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, expectedNonce string) (*idtoken.Claims, error)
	// ProviderName is the label its button carries. It is what makes
	// OIDC_PROVIDER_NAME reach the login screen, so a deployment behind
	// Authentik or Keycloak says so instead of "single sign-on".
	ProviderName() string
}

// SSORedirect is what the client sends the browser to, plus the binding secret
// it must keep and hand back at the exchange.
type SSORedirect struct {
	URL string `json:"url"`
	// Binding never travels to the provider and never appears in a URL: the
	// client stores it and presents it when collecting the session.
	Binding string `json:"binding"`
}

// SSOCallback is what a provider callback carries that is not already held
// server-side under the state. FirstName and LastName exist for Apple, which
// sends the name with the callback and never inside the ID token.
type SSOCallback struct {
	Provider  string
	Code      string
	State     string
	FirstName string
	LastName  string
	IPAddress string
	UserAgent string
}

// ssoFlow is the server-side half of one authorization request, keyed by state.
// Holding the verifier and nonce here is what makes them single-use, and
// Provider is stored with them so a state minted for one provider cannot be
// presented at another provider's callback.
//
// Binding is the secret that ties the flow to the browser that started it (see
// SSOExchange).
type ssoFlow struct {
	Provider string `json:"provider"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	Binding  string `json:"binding"`
	// Purpose is empty for a sign-in and ssoPurposeReauth for a signed-in
	// session re-proving its holder, which also pins the user and session.
	Purpose   string    `json:"purpose,omitempty"`
	UserID    uuid.UUID `json:"user_id"`
	SessionID uuid.UUID `json:"session_id"`
	// ReturnOrigin is the verified portal domain the flow began on; empty returns to APP_URL.
	ReturnOrigin string `json:"return_origin,omitempty"`
}

// SSOCallbackResult is where the callback sends the browser: Reauth is set
// whenever the state belonged to a re-authentication, error or not.
type SSOCallbackResult struct {
	Code   string
	Reauth bool
}

// ssoHandoff is a completed login waiting to be collected, carrying the same
// binding forward so only the browser that began the flow can collect it.
type ssoHandoff struct {
	Binding string             `json:"binding"`
	Result  models.LoginResult `json:"result"`
}

// The cache keys keep the oidc_ prefix they shipped with: renaming them would
// strand every authorization already in flight across a deploy.
func ssoStateKey(state string) string { return "oidc_state:" + state }

// ssoHandoffTTL is how long the dashboard has to exchange the handoff code for
// the real session. Seconds, not minutes: the redirect and the exchange happen
// back to back.
const ssoHandoffTTL = 60 * time.Second

func ssoHandoffKey(code string) string { return "oidc_handoff:" + code }

// WireFederatedProvider attaches one browser sign-in provider under its
// identity-provider key ("oidc", "google", "apple"). A nil provider is ignored,
// so an unconfigured one simply stays unavailable.
func (s *authService) WireFederatedProvider(name string, p FederatedProvider) {
	if p == nil || name == "" {
		return
	}
	if s.providers == nil {
		s.providers = map[string]FederatedProvider{}
	}
	s.providers[name] = p
}

// federatedProviderOrder is the order the login screen renders the buttons in.
var federatedProviderOrder = []string{
	models.IdentityProviderGoogle,
	models.IdentityProviderApple,
	models.IdentityProviderOIDC,
}

// FederatedProviders lists the configured providers, so /auth/config can
// advertise exactly the buttons this deployment can actually complete.
func (s *authService) FederatedProviders() []string {
	out := make([]string, 0, len(s.providers))
	for _, name := range federatedProviderOrder {
		if _, ok := s.providers[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// FederatedProviderLabels is what each button should say.
func (s *authService) FederatedProviderLabels() map[string]string {
	if len(s.providers) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.providers))
	for _, name := range federatedProviderOrder {
		if p, ok := s.providers[name]; ok && p.ProviderName() != "" {
			out[name] = p.ProviderName()
		}
	}
	return out
}

// mintHandoff stores a completed login behind a single-use code. The callback
// answers a browser, so it must redirect; tokens in that URL would leak into
// history, Referer and proxy logs, so it carries an opaque code instead.
func (s *authService) mintHandoff(ctx context.Context, result *models.LoginResult, binding string) (string, *errx.Error) {
	code, err := randomHex(32)
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	payload, err := json.Marshal(ssoHandoff{Binding: binding, Result: *result})
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	if err := s.cache.SetEx(ctx, ssoHandoffKey(code), payload, ssoHandoffTTL).Err(); err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	return code, nil
}

// SSOExchange swaps a handoff code for the session it stands for. Single use:
// the code is deleted as it is read.
//
// binding makes the handoff non-transferable. One-time state proves the
// callback answers a request this server made, not one THIS browser made, so
// without it a flow run against an attacker's own provider account could be
// forwarded to sign that person into the attacker's workspace (RFC 9700 4.7.1).
// The secret never leaves the initiating browser and never appears in a URL.
func (s *authService) SSOExchange(ctx context.Context, code, binding string) (*models.LoginResult, *errx.Error) {
	if code == "" || binding == "" {
		return nil, errx.ErrSSOBrowser
	}
	raw, err := s.cache.GetDel(ctx, ssoHandoffKey(code)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, errx.ErrToken
	}
	var handoff ssoHandoff
	if err := json.Unmarshal(raw, &handoff); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if subtle.ConstantTimeCompare([]byte(handoff.Binding), []byte(binding)) != 1 {
		return nil, errx.ErrSSOBrowser
	}
	return &handoff.Result, nil
}

// SSOBegin starts an authorization request against one provider.
func (s *authService) SSOBegin(ctx context.Context, provider string) (*SSORedirect, *errx.Error) {
	p, ok := s.providers[provider]
	if !ok || p == nil {
		return nil, errx.ErrExternalProvider
	}

	return s.beginFlow(ctx, p, ssoFlow{Provider: provider}, "")
}

// beginFlow mints the state, nonce, verifier and binding for one authorization
// and stores them under the state. statePrefix only routes the callback; the
// stored Purpose is what is trusted.
func (s *authService) beginFlow(ctx context.Context, p FederatedProvider, flow ssoFlow, statePrefix string) (*SSORedirect, *errx.Error) {
	state, err := randomHex(32)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	state = statePrefix + state
	nonce, err := randomHex(32)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	verifier, err := randomHex(32)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	binding, err := randomHex(32)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	flow.Verifier, flow.Nonce, flow.Binding = verifier, nonce, binding
	flow.ReturnOrigin = config.PortalOrigin(config.DashboardOriginFromContext(ctx))
	payload, err := json.Marshal(flow)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if err := s.cache.SetEx(ctx, ssoStateKey(state), payload, ssoStateTTL).Err(); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &SSORedirect{URL: p.AuthCodeURL(state, nonce, verifier), Binding: binding}, nil
}

// SSOReturnOrigin is the verified portal origin a flow began on, read without consuming its state; empty means APP_URL.
func (s *authService) SSOReturnOrigin(ctx context.Context, state string) string {
	if state == "" {
		return ""
	}
	raw, err := s.cache.Get(ctx, ssoStateKey(state)).Bytes()
	if err != nil || len(raw) == 0 {
		return ""
	}
	var flow ssoFlow
	if json.Unmarshal(raw, &flow) != nil {
		return ""
	}
	return config.PortalOrigin(flow.ReturnOrigin)
}

// SSOCallbackComplete finishes an authorization and returns a single-use
// handoff code the dashboard exchanges for the session.
func (s *authService) SSOCallbackComplete(ctx context.Context, in SSOCallback) (*SSOCallbackResult, *errx.Error) {
	out := &SSOCallbackResult{Reauth: IsReauthState(in.State)}
	code, err := s.ssoCallbackComplete(ctx, in, out.Reauth)
	out.Code = code
	return out, err
}

func (s *authService) ssoCallbackComplete(ctx context.Context, in SSOCallback, reauthState bool) (string, *errx.Error) {
	p, ok := s.providers[in.Provider]
	if !ok || p == nil {
		return "", errx.ErrExternalProvider
	}
	if in.Code == "" || in.State == "" {
		return "", errx.ErrExternalCode
	}

	// Consume the state before using it, so a replayed callback finds nothing.
	raw, cerr := s.cache.GetDel(ctx, ssoStateKey(in.State)).Bytes()
	if cerr != nil || len(raw) == 0 {
		return "", errx.ErrExternalCode
	}

	var flow ssoFlow
	if err := json.Unmarshal(raw, &flow); err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	// A state minted for one provider presented at another's callback is a
	// mix-up attack (RFC 9700 4.4).
	if flow.Provider != "" && flow.Provider != in.Provider {
		return "", errx.ErrExternalCode
	}
	// The stored purpose decides, and it must agree with the state's routing prefix.
	isReauth := flow.Purpose == ssoPurposeReauth
	if isReauth != reauthState {
		return "", errx.ErrExternalCode
	}

	claims, err := p.Exchange(ctx, in.Code, flow.Verifier, flow.Nonce)
	if err != nil {
		// Reported, not buried: these are configuration problems more often
		// than attacks.
		errs.CaptureException(err)
		return "", errx.ErrExternalCode
	}

	if isReauth {
		return s.completeReauthFlow(ctx, flow, claims)
	}

	email, perr := mail.ParseAddress(claims.Email)
	if perr != nil {
		return "", errx.ErrExternalEmail
	}

	firstName, lastName := claims.GivenName, claims.FamilyName
	if firstName == "" {
		firstName = in.FirstName
	}
	if lastName == "" {
		lastName = in.LastName
	}

	res, rerr := s.resolveFederatedUser(
		ctx,
		in.Provider,
		claims.Issuer,
		claims.Subject,
		email,
		firstName,
		lastName,
	)
	if rerr != nil {
		return "", rerr
	}

	// The callback carries no Origin, so a flow begun on a portal mints its session for that portal.
	if origin := config.PortalOrigin(flow.ReturnOrigin); origin != "" {
		ctx = config.WithDashboardOrigin(ctx, origin)
	}

	// Carried through the handoff like a 2FA challenge is: the dashboard
	// collects it at the exchange and asks for the password there.
	result, lerr := s.finishFederatedLogin(ctx, res, in.IPAddress, in.UserAgent, sessionProvider(in.Provider))
	if lerr != nil {
		return "", lerr
	}
	return s.mintHandoff(ctx, result, flow.Binding)
}

// sessionProvider is how the session records what the person signed in with,
// shown on the account security page.
func sessionProvider(provider string) string {
	switch provider {
	case models.IdentityProviderGoogle:
		return token.AuthProviderGoogle
	case models.IdentityProviderApple:
		return token.AuthProviderApple
	default:
		return token.AuthProviderOIDC
	}
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
