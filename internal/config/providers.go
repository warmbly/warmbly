package config

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// Provider selection helpers. These centralize the env-var switches and their
// defaults so the service mains (and the AWS-config gate below) agree on which
// backend is active without each re-deriving it.

// KMSProvider returns the selected KMS provider ("aws" | "local"). Default aws.
func KMSProvider() string { return providerOr("KMS_PROVIDER", "aws") }

// BlobProvider returns the selected blob provider ("s3" | "filesystem").
// Default s3.
func BlobProvider() string { return providerOr("BLOB_PROVIDER", "s3") }

// EventBusProvider returns the selected event bus ("kafka" | "nats").
// Default kafka.
func EventBusProvider() string { return providerOr("EVENTBUS_PROVIDER", "kafka") }

// EventBusUsesKafka includes the old transport kept for a staged migration.
func EventBusUsesKafka() bool {
	readBoth, _ := strconv.ParseBool(os.Getenv("EVENTBUS_READ_BOTH"))
	return EventBusProvider() == "kafka" || readBoth
}

// CodecProvider returns the selected codec ("avro" | "json"). Default avro.
func CodecProvider() string { return providerOr("CODEC_PROVIDER", "avro") }

// TasksProvider returns the selected task scheduler ("local" | "gcloud").
// Default local — self-host runs the in-process Postgres dispatcher and needs
// no GCP Cloud Tasks.
func TasksProvider() string { return providerOr("TASKS_PROVIDER", "local") }

// BillingProvider returns the selected billing backend ("none" | "stripe").
// Default none — self-host boots without Stripe and unlocks all features.
func BillingProvider() string { return providerOr("BILLING_PROVIDER", "none") }

// Mail transports for platform email (login codes, resets, invites, digests).
const (
	MailTransportSMTP = "smtp"
	MailTransportLog  = "log"
	MailTransportSES  = "ses"
)

// MailTransport returns the selected platform-mail transport. Explicit
// MAIL_TRANSPORT wins; otherwise SMTP_HOST selects smtp and an unset one falls
// back to ses, which is what hosted deployments have always used. Self-host
// compose sets log explicitly so a fresh install can log in with no relay.
func MailTransport() string {
	if v := os.Getenv("MAIL_TRANSPORT"); v != "" {
		return strings.ToLower(strings.TrimSpace(v))
	}
	if os.Getenv("SMTP_HOST") != "" {
		return MailTransportSMTP
	}
	return MailTransportSES
}

// SelfHosted reports whether this deployment is a self-host install.
// DEPLOYMENT_MODE is the explicit switch; BILLING_PROVIDER=none is the historic
// signal and stays the fallback so existing installs keep their behavior.
func SelfHosted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEPLOYMENT_MODE"))) {
	case "self_hosted", "selfhosted", "self-hosted":
		return true
	case "cloud", "hosted":
		return false
	}
	return BillingProvider() == "none"
}

// CaptchaProvider returns the selected captcha backend ("turnstile" | "none").
// Explicit CAPTCHA_PROVIDER wins; otherwise it's "turnstile" only when a
// TURNSTILE_SECRET is present, so a self-host that sets no secret runs with
// captcha off instead of a broken verify.
func CaptchaProvider() string {
	if v := os.Getenv("CAPTCHA_PROVIDER"); v != "" {
		return v
	}
	if !TurnstileSecretConfigured() {
		return "none"
	}
	return "turnstile"
}

// turnstileSecretResolved is set when the auth config resolved a secret from
// any source, including a secret store the environment does not show.
var turnstileSecretResolved atomic.Bool

// SetTurnstileSecretResolved records whether the auth config resolved a secret.
func SetTurnstileSecretResolved(ok bool) { turnstileSecretResolved.Store(ok) }

// TurnstileSecretConfigured reports whether a Turnstile secret is available.
func TurnstileSecretConfigured() bool {
	return os.Getenv("TURNSTILE_SECRET") != "" || turnstileSecretResolved.Load()
}

// AWSNeeded reports whether any AWS-backed provider is selected, so a fully
// local deployment (KMS_PROVIDER=local + BLOB_PROVIDER=filesystem) can skip
// loading the AWS SDK config entirely and never needs AWS_REGION / credentials.
func AWSNeeded() bool {
	return KMSProvider() == "aws" || BlobProvider() == "s3"
}

func providerOr(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

// TurnstileSiteKey is the public widget key the hosted form page renders
// with. Frontends carry their own copy (TURNSTILE_KEY); this one exists
// because the server-rendered page cannot read the SPA's runtime config.
func TurnstileSiteKey() string {
	return strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY"))
}

// MailhostISPDB reports whether mailbox server detection may ask Thunderbird's
// ISPDB (autoconfig.thunderbird.net), which sends it the domain name. On by
// default for the hosted product and off for a self-host; MAILHOST_ISPDB overrides.
func MailhostISPDB() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAILHOST_ISPDB"))) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return !SelfHosted()
}

// DevEnv reports whether APP_ENV names a development instance; unset is dev.
func DevEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
	case "", "dev", "development", "local":
		return true
	}
	return false
}

// ExplicitDevelopment reports whether APP_ENV names development; unlike DevEnv, unset is not.
func ExplicitDevelopment() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
	case "dev", "development", "local":
		return true
	}
	return false
}

// HostedProduction reports a hosted (not self-host) instance outside development.
func HostedProduction() bool { return !SelfHosted() && !DevEnv() }

// SMTPCRAMMD5Allowed preserves legacy mail-server compatibility outside hosted production.
func SMTPCRAMMD5Allowed() bool { return !HostedProduction() }

// UnsafeWebhookURLsAllowed reports whether outbound HTTP to customer URLs may use
// http:// and private addresses. Only a self-hosted or development instance may opt in.
func UnsafeWebhookURLsAllowed() bool {
	return isTrue(os.Getenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS")) && !HostedProduction()
}

// PrivateMailHostsAllowed reports whether mailbox SMTP/IMAP connections may reach
// non-public addresses. Never on hosted production; a self-hosted or development
// instance allows them (on-premises mail servers) unless MAIL_ALLOW_PRIVATE_HOSTS=false.
func PrivateMailHostsAllowed() bool {
	if HostedProduction() {
		return false
	}
	return !isFalse(os.Getenv("MAIL_ALLOW_PRIVATE_HOSTS"))
}

// MailTLSInsecureAllowed reports whether MAIL_TLS_INSECURE=true applies: never
// on hosted production, where a mailbox opts out per server with a pinned key.
func MailTLSInsecureAllowed() bool {
	return isTrue(os.Getenv("MAIL_TLS_INSECURE")) && !HostedProduction()
}

// SMTPTLSInsecureAllowed reports whether SMTP_TLS_INSECURE_SKIP_VERIFY=true applies: never on hosted production.
func SMTPTLSInsecureAllowed() bool {
	return isTrue(os.Getenv("SMTP_TLS_INSECURE_SKIP_VERIFY")) && !HostedProduction()
}

// MailvendorSandboxURL points every inbox vendor client at the sandbox's mock
// vendor API (<url>/<vendor>). Honoured only in development (APP_ENV dev or
// unset), so a stray value can never send real vendor keys elsewhere.
func MailvendorSandboxURL() string {
	if env := strings.TrimSpace(os.Getenv("APP_ENV")); env != "" && !strings.EqualFold(env, "dev") {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("MAILVENDOR_SANDBOX_URL")), "/")
}
