package observability

import (
	"context"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/version"
)

// Init configures error reporting for one Go service.
//
// It resolves the two things a service knows only at boot, the operator's
// backend credentials and the build it is running, and hands them to the SDK
// wrapper. Configuring neither backend is not an error: reporting is optional
// in every environment, including prod, and an unconfigured service logs its
// errors locally instead.
func Init(ctx context.Context, cfg *config.Config, service string) error {
	applyLogLevel(cfg.Env)
	dsn, err := cfg.LoadSentryDSNBackend(ctx)
	if err != nil {
		dsn = ""
	}

	var posthogKey string
	if config.PostHogErrorTracking() {
		posthogKey = cfg.LoadPostHogKey(ctx)
	}

	return errs.Init(errs.Config{
		PostHogKey:  posthogKey,
		PostHogHost: config.PostHogHost(),
		SentryDSN:   dsn,
		Environment: cfg.Env,
		Release:     Release(),
		Service:     service,
	})
}

// InitEnv configures error reporting for a service that reads its own
// environment instead of the shared config loader. The forms service has no
// database and no secrets backend, so it has no config.Config to consult, and
// building one just to read POSTHOG_KEY would give it both.
func InitEnv(service string) error {
	env := strings.TrimSpace(os.Getenv("APP_ENV"))
	if env == "" {
		env = "dev"
	}
	applyLogLevel(env)

	var posthogKey string
	if config.PostHogErrorTracking() {
		posthogKey = strings.TrimSpace(os.Getenv("POSTHOG_KEY"))
	}

	return errs.Init(errs.Config{
		PostHogKey:  posthogKey,
		PostHogHost: config.PostHogHost(),
		SentryDSN:   strings.TrimSpace(os.Getenv("SENTRY_DSN")),
		Environment: env,
		Release:     Release(),
		Service:     service,
	})
}

// Release is the value every service tags its events with, so a stack trace
// names the build it came from. It is the stamped release when the build
// stamped one and the short commit otherwise; an unstamped local build reports
// "dev", which is what version.String falls back to.
func Release() string {
	if v := version.String(); v != "" && v != "dev" {
		return v
	}
	if c := version.ShortCommit(); c != "" {
		return c
	}
	return "dev"
}

// applyLogLevel sets the process log level from LOG_LEVEL, defaulting to debug in dev and info elsewhere.
func applyLogLevel(env string) {
	level := zerolog.InfoLevel
	if env == "dev" {
		level = zerolog.DebugLevel
	}
	if v := strings.TrimSpace(os.Getenv("LOG_LEVEL")); v != "" {
		if parsed, err := zerolog.ParseLevel(strings.ToLower(v)); err == nil && parsed != zerolog.NoLevel {
			level = parsed
		}
	}
	zerolog.SetGlobalLevel(level)
}
