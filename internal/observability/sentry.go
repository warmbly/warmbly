package observability

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/config"
)

// InitSentry configures Sentry for all runtime environments.
// In non-prod environments, events are logged locally via BeforeSend.
func InitSentry(ctx context.Context, cfg *config.Config, service string) error {
	options := sentry.ClientOptions{
		SendDefaultPII: true,
		Environment:    cfg.Env,
		ServerName:     service,
	}

	if cfg.Env == "prod" {
		sentryDsn, err := cfg.LoadSentryDSNBackend(ctx)
		if err != nil {
			return err
		}
		options.Dsn = sentryDsn
	} else {
		options.BeforeSend = func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			log.Printf("[sentry-local][%s][%s] %s", service, event.Level, summarizeSentryEvent(event))
			return event
		}
	}

	if err := sentry.Init(options); err != nil {
		return fmt.Errorf("init sentry: %w", err)
	}

	return nil
}

func summarizeSentryEvent(event *sentry.Event) string {
	if event == nil {
		return "nil event"
	}

	parts := []string{}
	if event.EventID != "" {
		parts = append(parts, "event_id="+string(event.EventID))
	}
	if event.Message != "" {
		parts = append(parts, "message="+event.Message)
	}
	if len(event.Exception) > 0 {
		ex := event.Exception[0]
		exMsg := strings.TrimSpace(strings.TrimSpace(ex.Type + ": " + ex.Value))
		if exMsg != "" && exMsg != ":" {
			parts = append(parts, "exception="+exMsg)
		}
	}
	if len(parts) == 0 {
		return "captured event with no message"
	}

	return strings.Join(parts, " | ")
}
