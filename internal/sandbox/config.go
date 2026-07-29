// Package sandbox seeds and animates a fully working local demo environment:
// a paid showcase org whose mailboxes really send (SMTP -> mailpit) and really
// sync (IMAP <- dovecot), plus a simulator that plays "the internet" - it
// routes captured mail into recipient inboxes, opens tracking pixels, clicks
// tracked links, and writes replies as the seeded contacts. Every platform
// code path involved (scheduler, worker, consumer, tracking, realtime) is the
// production one; only the humans are simulated.
//
// See docs/content/docs/development/sandbox.mdx for the full walkthrough.
package sandbox

import (
	"os"
	"strconv"
)

// Config carries the endpoints the seeder and simulator talk to. Defaults
// match the `make infra` host-published ports.
type Config struct {
	// DatabaseURL is the dev Postgres DSN (seeding + entity lookups).
	DatabaseURL string

	// MailpitURL is the mailpit HTTP API base (message capture).
	MailpitURL string

	// TrackingURL is the tracking service base for pixel/click hits.
	TrackingURL string

	// IMAPAddr is the dovecot IMAPS address the SIMULATOR appends mail to.
	IMAPAddr string
	// IMAPPassword is dovecot's static password (any username works).
	IMAPPassword string

	// SMTPHost/SMTPPort are seeded into sandbox mailboxes as their outbound
	// server; the WORKER dials these, so they are host-relative (mailpit).
	SMTPHost string
	SMTPPort int
	// IMAPHost/IMAPPort are seeded as the mailboxes' inbound server; the
	// WORKER dials these too (dovecot IMAPS).
	IMAPHost string
	IMAPPort int

	// CredentialsKey is the CREDENTIALS_ENCRYPTION_KEY hex used to seal the
	// seeded SMTP/IMAP credentials; must match the backend's key.
	CredentialsKey string
}

// FromEnv builds a Config from the environment with `make infra` defaults.
func FromEnv() Config {
	return Config{
		DatabaseURL:    getenv("PRIMARY_DB", "postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable"),
		MailpitURL:     getenv("MAILPIT_URL", "http://localhost:18025"),
		TrackingURL:    getenv("TRACKING_URL", "http://localhost:3000"),
		IMAPAddr:       getenv("DOVECOT_IMAP_ADDR", "localhost:10993"),
		IMAPPassword:   getenv("DOVECOT_PASSWORD", "sandbox"),
		SMTPHost:       getenv("SANDBOX_SMTP_HOST", "localhost"),
		SMTPPort:       getenvInt("SANDBOX_SMTP_PORT", 11025),
		IMAPHost:       getenv("SANDBOX_IMAP_HOST", "localhost"),
		IMAPPort:       getenvInt("SANDBOX_IMAP_PORT", 10993),
		CredentialsKey: os.Getenv("CREDENTIALS_ENCRYPTION_KEY"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
