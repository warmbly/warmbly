package config

import (
	"os"
	"strings"
)

// Login-code modes. Emailing a code on every login makes the mail relay a
// single point of failure for all authentication, and NIST SP 800-63B and OWASP
// ASVS both decline to count email as a second factor. The code stays valuable
// for address validation and recovery, so the setting governs the login step
// only.
const (
	LoginCodeAlways    = "always"
	LoginCodeNewDevice = "new_device"
	LoginCodeOff       = "off"
)

// Registration modes, using Plausible's vocabulary. Tri-state rather than a
// boolean because "closed to the public but invitations still work" is the
// state most operators actually want.
const (
	RegistrationOpen       = "false"
	RegistrationInviteOnly = "invite_only"
	RegistrationClosed     = "true"
)

// AuthPolicy is the per-deployment auth behavior that self-host and cloud need
// to differ on. Every value is independently overridable; DEPLOYMENT_MODE only
// picks the defaults.
type AuthPolicy struct {
	// LoginCode is one of the LoginCode* constants.
	LoginCode string
	// RequireEmailVerification gates whether registration must complete an
	// emailed code before the account exists.
	RequireEmailVerification bool
	// Registration is one of the Registration* constants.
	Registration string
	// DisablePasswordLogin turns off email+password entirely, for deployments
	// that authenticate through OIDC only.
	DisablePasswordLogin bool
}

// LoadAuthPolicy resolves the policy from the environment. mailDelivers is
// whether the configured transport actually puts mail on the wire: when it does
// not, an emailed login code cannot be a hard requirement, because there would
// be no way to complete a login at all.
func LoadAuthPolicy(mailDelivers bool) *AuthPolicy {
	selfHost := SelfHosted()

	loginCode := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_LOGIN_CODE")))
	switch loginCode {
	case LoginCodeAlways, LoginCodeNewDevice, LoginCodeOff:
	default:
		if selfHost {
			loginCode = LoginCodeOff
		} else {
			loginCode = LoginCodeNewDevice
		}
	}
	// A transport that does not deliver cannot gate logins, whatever the
	// operator asked for. Codes still reach them through the log transport, so
	// this only removes the hard dependency, not the audit trail.
	if !mailDelivers && loginCode == LoginCodeAlways {
		loginCode = LoginCodeNewDevice
	}

	requireVerification := !selfHost
	if v := os.Getenv("REQUIRE_EMAIL_VERIFICATION"); v != "" {
		requireVerification = isTrue(v)
	}

	registration := strings.ToLower(strings.TrimSpace(os.Getenv("DISABLE_REGISTRATION")))
	switch registration {
	case RegistrationOpen, RegistrationInviteOnly, RegistrationClosed:
	default:
		if selfHost {
			registration = RegistrationInviteOnly
		} else {
			registration = RegistrationOpen
		}
	}

	return &AuthPolicy{
		LoginCode:                loginCode,
		RequireEmailVerification: requireVerification,
		Registration:             registration,
		DisablePasswordLogin:     isTrue(os.Getenv("DISABLE_PASSWORD_LOGIN")),
	}
}

// PublicSignupsAllowed reports whether an uninvited stranger may create an
// account. The first-launch exemption is applied by the caller, which knows
// whether any user exists yet.
func (p *AuthPolicy) PublicSignupsAllowed() bool {
	return p.Registration == RegistrationOpen
}

// InvitesAllowed reports whether an existing member may still invite people.
// Only the fully closed mode stops that; invite_only exists precisely to keep
// it working.
func (p *AuthPolicy) InvitesAllowed() bool {
	return p.Registration != RegistrationClosed
}
