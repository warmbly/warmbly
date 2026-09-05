package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/config"
)

// DeploymentAuthConfig is what the login screen needs to render truthfully.
//
// Without it the frontends guess: the Turnstile widget mounted even when
// captcha was off server-side, social buttons rendered with no client
// configured, and nothing told a self-hoster their login code was going to a
// log file. Everything here is public, non-secret configuration.
type DeploymentAuthConfig struct {
	// Captcha reports whether a Turnstile token is actually verified. When
	// false the client must not mount the widget: a self-hosted or air-gapped
	// install cannot reach challenges.cloudflare.com.
	Captcha bool `json:"captcha"`

	// PasswordLogin is false when the deployment authenticates only through
	// OIDC or passkeys.
	PasswordLogin bool `json:"password_login"`

	// LoginCode is always, new_device or off. The client uses it to decide
	// whether to expect a code step, and to explain the flow up front.
	LoginCode string `json:"login_code"`

	// Registration is false, invite_only or true, already resolved through the
	// first-launch exemption, so a brand new instance reports open signups.
	Registration string `json:"registration"`

	// EmailVerification reports whether a signup must confirm an emailed code.
	EmailVerification bool `json:"email_verification"`

	// MailDelivers is false when the platform mail transport does not put mail
	// on the wire (MAIL_TRANSPORT=log). The login screen tells the operator
	// where to find codes instead of leaving them waiting for an email.
	MailDelivers bool `json:"mail_delivers"`

	// Passkeys reports whether WebAuthn can work here at all. It needs a
	// secure context, so a plain-http LAN origin disables it rather than
	// failing in the browser with an opaque error.
	Passkeys bool `json:"passkeys"`

	Providers []string `json:"providers"`

	// ProviderLabels is what each button should say, keyed by the same
	// identifiers, so a deployment behind Authentik says so rather than
	// naming a protocol.
	ProviderLabels map[string]string `json:"provider_labels,omitempty"`

	// SelfHosted lets the UI drop hosted-only affordances (billing prompts,
	// referral fields) that make no sense on someone's own server.
	SelfHosted bool `json:"self_hosted"`

	// BillingEnabled mirrors the backend feature gate exactly: false when
	// BILLING_PROVIDER=none, in which case every feature is unlocked and the
	// dashboard must not present the org as being on a trial or free tier.
	BillingEnabled bool `json:"billing_enabled"`

	// SetupRequired is true while the instance has no accounts at all. The
	// login screen redirects to the setup page rather than showing a form
	// nobody can yet use.
	SetupRequired bool `json:"setup_required"`

	// InvitesRequired mirrors registration == invite_only, precomputed so the
	// client does not reimplement the meaning of a tri-state string.
	InvitesRequired bool `json:"invites_required"`

	// DocsURL is where to send someone whose signup was refused by deployment
	// policy rather than by anything they did wrong.
	DocsURL string `json:"docs_url"`

	// WebsocketURL is the realtime gateway. Served here because a developer
	// client (the CLI's event stream, an SDK) has no other way to find the
	// socket on a self-hosted instance. Empty when the instance runs no
	// realtime service.
	WebsocketURL string `json:"websocket_url,omitempty"`

	// AppURL is the dashboard origin, the same one every emailed link is built
	// from. A client that wants to send someone to a page (the CLI's `browse`,
	// a chat integration) cannot derive it: on a self-hosted instance the host
	// layout is whatever the operator chose.
	AppURL string `json:"app_url,omitempty"`
}

// accountsDocsURL is the page every registration refusal points at.
const accountsDocsURL = "https://docs.warmbly.com/development/accounts-and-access/"

// AuthConfig serves GET /v1/auth/config. Public and unauthenticated by design:
// it is the first request the login screen makes.
func (h *Handler) AuthConfig(c *gin.Context) {
	policy := h.AuthService.Policy()

	// Only providers this backend can complete a browser sign-in with. A
	// native iOS client id is not one of them; native apps read
	// /auth/providers.
	providers := h.AuthService.FederatedProviders()

	registration := h.AuthService.RegistrationMode(c.Request.Context())

	c.JSON(http.StatusOK, DeploymentAuthConfig{
		Captcha:           config.CaptchaProvider() != "none",
		PasswordLogin:     !policy.DisablePasswordLogin,
		LoginCode:         policy.LoginCode,
		Registration:      registration,
		EmailVerification: policy.RequireEmailVerification,
		MailDelivers:      h.MailDelivers,
		Passkeys:          h.PasskeysUsable,
		Providers:         providers,
		ProviderLabels:    h.AuthService.FederatedProviderLabels(),
		SelfHosted:        config.SelfHosted(),
		BillingEnabled:    config.BillingProvider() != "none",
		SetupRequired:     h.BootstrapService != nil && h.BootstrapService.Required(c.Request.Context()),
		InvitesRequired:   registration == config.RegistrationInviteOnly,
		DocsURL:           accountsDocsURL,
		WebsocketURL:      config.WebsocketURL(),
		AppURL:            config.AppBaseURL(),
	})
}
