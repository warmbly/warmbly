package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

// GraphKit is the partner API the store buys domains and mailboxes through.
// The store is off unless GRAPHKIT_API_KEY is set; GRAPHKIT_API_URL=fake runs
// the in-process simulator and needs no key, in development only.
type GraphKit struct {
	APIURL                string
	APIKey                string
	WebhookSecret         string
	WebhookSecretPrevious string
	// WebhookSecretRotatedAt is when a connection saved in the admin panel replaced its webhook secret; the environment sets none.
	WebhookSecretRotatedAt *time.Time
}

// LoadGraphKit reads the GRAPHKIT_* environment.
func LoadGraphKit() GraphKit {
	return GraphKit{
		APIURL:                strings.TrimSpace(os.Getenv("GRAPHKIT_API_URL")),
		APIKey:                strings.TrimSpace(os.Getenv("GRAPHKIT_API_KEY")),
		WebhookSecret:         strings.TrimSpace(os.Getenv("GRAPHKIT_WEBHOOK_SECRET")),
		WebhookSecretPrevious: strings.TrimSpace(os.Getenv("GRAPHKIT_WEBHOOK_SECRET_PREVIOUS")),
	}
}

// Fake reports the simulator.
func (g GraphKit) Fake() bool { return strings.EqualFold(g.APIURL, "fake") }

// Sandbox reports a GraphKit test key: everything upstream is simulated, and
// its brokered Google tokens are opaque placeholders Google would refuse.
func (g GraphKit) Sandbox() bool { return strings.HasPrefix(g.APIKey, "gk_test_") }

// Enabled reports whether the store is configured.
func (g GraphKit) Enabled() bool { return g.APIKey != "" || g.Fake() }

// Validate refuses the simulator outside development, where it would sell nothing real.
func (g GraphKit) Validate() error {
	if g.Fake() && !DevEnv() {
		return errors.New("GRAPHKIT_API_URL=fake is a development simulator and is refused outside APP_ENV=development")
	}
	return nil
}
