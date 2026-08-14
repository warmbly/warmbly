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

	// SelfHosted lets the UI drop hosted-only affordances (billing prompts,
	// referral fields) that make no sense on someone's own server.
	SelfHosted bool `json:"self_hosted"`

	// SetupRequired is true while the instance has no accounts at all. The
	// login screen redirects to the setup page rather than showing a form
	// nobody can yet use.
	SetupRequired bool `json:"setup_required"`
}

// AuthConfig serves GET /v1/auth/config. Public and unauthenticated by design:
// it is the first request the login screen makes.
func (h *Handler) AuthConfig(c *gin.Context) {
	policy := h.AuthService.Policy()

	providers := []string{}
	if h.ExternalAuthProviders.GoogleIOSClientID != "" || h.GoogleWebSignIn {
		providers = append(providers, "google")
	}
	if h.ExternalAuthProviders.AppleBundleID != "" || h.AppleWebSignIn {
		providers = append(providers, "apple")
	}
	if h.OIDCEnabled {
		providers = append(providers, "oidc")
	}

	c.JSON(http.StatusOK, DeploymentAuthConfig{
		Captcha:           config.CaptchaProvider() != "none",
		PasswordLogin:     !policy.DisablePasswordLogin,
		LoginCode:         policy.LoginCode,
		Registration:      h.AuthService.RegistrationMode(c.Request.Context()),
		EmailVerification: policy.RequireEmailVerification,
		MailDelivers:      h.MailDelivers,
		Passkeys:          h.PasskeysUsable,
		Providers:         providers,
		SelfHosted:        config.SelfHosted(),
		SetupRequired:     h.BootstrapService != nil && h.BootstrapService.Required(c.Request.Context()),
	})
}
