// Package instanceconfig describes the environment the running backend
// resolved, so an operator can see what a variable actually became.
//
// It never reads a second source of truth: every resolver calls the same
// config helper the runtime uses, so the page cannot drift from behaviour.
// Nothing here writes: the environment is authoritative.
package instanceconfig

import (
	"os"
	"strings"

	"github.com/warmbly/warmbly/internal/config"
)

// Groups the admin panel renders as sections.
const (
	GroupDeployment    = "deployment"
	GroupAddresses     = "addresses"
	GroupDatabase      = "database"
	GroupCache         = "cache"
	GroupMail          = "mail"
	GroupAuth          = "auth"
	GroupEncryption    = "encryption"
	GroupStorage       = "storage"
	GroupEventBus      = "eventbus"
	GroupWorkers       = "workers"
	GroupTracking      = "tracking"
	GroupCaptcha       = "captcha"
	GroupObservability = "observability"
)

// Where a resolved value came from.
const (
	SourceEnv     = "env"
	SourceDefault = "default"
	SourceDerived = "derived"
	SourceUnset   = "unset"
)

// Whether a change needs a restart.
const (
	ChangeBootOnly   = "boot-only"
	ChangePerRequest = "per-request"
)

// PublishedDefaults are the working secret values shipped in docker-compose.yml
// and the Makefile. They are public in this repository, so a deployment still
// running on one is not protected by it at all.
//
// This mirrors insecureDefaults in cmd/backend/boot.go; the two must stay
// identical or the health check and the boot refusal will disagree.
var PublishedDefaults = map[string]string{
	"AUTH_SECRET":                "local-dev-auth-secret-minimum-32-characters-long",
	"KMS_LOCAL_MASTER_KEY":       "Xr0JA7gqF2POy29a7MRByyqddivTNt8WOyKsOXklazk=",
	"CREDENTIALS_ENCRYPTION_KEY": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	"INTERNAL_API_TOKEN":         "local-dev-internal-token",
	"SECRET_KEY_BASE":            "local-development-secret-key-base-minimum-64-characters-for-phoenix",
}

// Runtime carries the facts only cmd/backend knows after boot: values it
// derived rather than read, and the outcome of its own preflight. A nil
// Runtime is valid; every resolver falls back to the environment.
type Runtime struct {
	CORSOrigins       []string
	WebAuthnRPID      string
	WebAuthnRPOrigins []string
	OIDCRedirectURL   string
	OIDCConfigured    bool
	// OIDCDiscoveryErr is the boot-time discovery failure, empty when discovery
	// succeeded or was never attempted.
	OIDCDiscoveryErr  string
	MailTransportKind string
	MailTransportDesc string
	MailDelivers      bool
	PasskeysUsable    bool
	// InsecureDefaults are the variables boot found still holding a published
	// default value.
	InsecureDefaults []string
	Policy           *config.AuthPolicy
	WebsocketURL     string
}

// Entry describes one self-host-relevant environment variable.
type Entry struct {
	Key string
	// Aliases are older names for the same setting. Any of them being set
	// makes the source env.
	Aliases []string
	Group   string
	// RuntimeChangeable is ChangeBootOnly or ChangePerRequest.
	RuntimeChangeable string
	// Effect is one sentence saying what the resolved value does.
	Effect     string
	DocsAnchor string
	// WhenUnset is the source reported when no variable is set but a value
	// still resolves. SourceDefault when empty.
	WhenUnset string
	Resolve   func(rt *Runtime) string
}

// Resolved is one row of GET /admin/instance/config.
type Resolved struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Source is one of the Source* constants.
	Source    string `json:"source"`
	Sensitive bool   `json:"sensitive"`
	Set       bool   `json:"set"`
	// Fingerprint is the first 4 hex characters of SHA-256 of a sensitive
	// value, so two services can be compared without either being disclosed.
	Fingerprint string `json:"fingerprint"`
	Group       string `json:"group"`
	Effect      string `json:"effect"`
	Docs        string `json:"docs"`
	// RuntimeChangeable is one of the Change* constants.
	RuntimeChangeable string `json:"runtime_changeable"`
}

// Entries resolves the whole table in declaration order.
func Entries(rt *Runtime) []Resolved {
	out := make([]Resolved, 0, len(table))
	for _, e := range table {
		out = append(out, e.resolve(rt))
	}
	return out
}

func (e Entry) resolve(rt *Runtime) Resolved {
	value := ""
	if e.Resolve != nil {
		value = strings.TrimSpace(e.Resolve(rt))
	}

	r := Resolved{
		Key:               e.Key,
		Group:             e.Group,
		Effect:            e.Effect,
		Docs:              e.DocsAnchor,
		RuntimeChangeable: e.RuntimeChangeable,
		Sensitive:         Sensitive(e.Key),
		Set:               e.envSet(),
	}
	r.Source = e.source(r.Set, value)

	// A sensitive value never leaves the process; the fingerprint is what an
	// operator compares across services.
	if r.Sensitive {
		r.Fingerprint = Fingerprint(value)
		return r
	}
	r.Value = value
	return r
}

func (e Entry) envSet() bool {
	if strings.TrimSpace(os.Getenv(e.Key)) != "" {
		return true
	}
	for _, alias := range e.Aliases {
		if strings.TrimSpace(os.Getenv(alias)) != "" {
			return true
		}
	}
	return false
}

func (e Entry) source(set bool, value string) string {
	if set {
		return SourceEnv
	}
	if value == "" {
		return SourceUnset
	}
	if e.WhenUnset != "" {
		return e.WhenUnset
	}
	return SourceDefault
}
