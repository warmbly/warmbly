package config

import (
	"log"
	"os"
	"strings"
	"sync"
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
	// SSOAutoProvision lets a verified identity-provider assertion create an
	// account regardless of Registration, for operators whose IdP is the gate.
	// Off by default: otherwise configuring OIDC silently reopens signup.
	SSOAutoProvision bool
}

// LoadAuthPolicy resolves the policy from the environment. mailDelivers is
// whether the configured transport actually puts mail on the wire: when it does
// not, an emailed login code cannot be a hard requirement, because there would
// be no way to complete a login at all.
func LoadAuthPolicy(mailDelivers bool) *AuthPolicy {
	selfHost := SelfHosted()

	loginCode := resolveLoginCode(os.Getenv("AUTH_LOGIN_CODE"), selfHost)
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

	registration := resolveRegistration(os.Getenv("DISABLE_REGISTRATION"), selfHost)

	return &AuthPolicy{
		LoginCode:                loginCode,
		RequireEmailVerification: requireVerification,
		Registration:             registration,
		DisablePasswordLogin:     isTrue(os.Getenv("DISABLE_PASSWORD_LOGIN")),
		SSOAutoProvision:         isTrue(os.Getenv("SSO_AUTO_PROVISION")),
	}
}

// resolveLoginCode maps AUTH_LOGIN_CODE onto a mode. Unset takes the
// deployment default, a boolean spelling is honoured, and anything else is a
// typo the operator is told about instead of being read as "not set".
func resolveLoginCode(raw string, selfHost bool) string {
	fallback := LoginCodeNewDevice
	if selfHost {
		fallback = LoginCodeOff
	}

	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return fallback
	case LoginCodeAlways, LoginCodeNewDevice, LoginCodeOff:
		return value
	}
	if isTrue(value) {
		return LoginCodeAlways
	}
	if isFalse(value) {
		return LoginCodeOff
	}

	warnUnrecognized("AUTH_LOGIN_CODE", raw, fallback)
	return fallback
}

// resolveRegistration maps DISABLE_REGISTRATION onto a mode. The same rule as
// AUTH_LOGIN_CODE: DISABLE_REGISTRATION=1 asked for closed and must not read
// as open just because it is not spelled "true".
func resolveRegistration(raw string, selfHost bool) string {
	fallback := RegistrationOpen
	if selfHost {
		fallback = RegistrationInviteOnly
	}

	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return fallback
	case RegistrationOpen, RegistrationInviteOnly, RegistrationClosed:
		return value
	}
	if isTrue(value) {
		return RegistrationClosed
	}
	if isFalse(value) {
		return RegistrationOpen
	}

	warnUnrecognized("DISABLE_REGISTRATION", raw, fallback)
	return fallback
}

// isFalse is the negative half of isTrue, so an unrecognized value can be told
// apart from an explicit off.
func isFalse(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// warnedVars keeps the warning to one line per variable: LoadAuthPolicy is
// called per admin config request, not only at boot.
var warnedVars sync.Map

func warnUnrecognized(key, value, resolved string) {
	if _, seen := warnedVars.LoadOrStore(key, true); seen {
		return
	}
	log.Printf("Warning: %s=%q is not a recognized value; falling back to %s. Fix the value: it is not being read as unset.", key, value, resolved)
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
