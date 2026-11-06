package errs

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentrySink reports to Sentry. It is on when SENTRY_DSN is set, and it is the
// backend an operator who already runs Sentry keeps using: PostHog is the
// default here, not a replacement forced on anybody.
type sentrySink struct{}

func newSentrySink(cfg Config) (sink, error) {
	err := sentry.Init(sentry.ClientOptions{
		Dsn: cfg.SentryDSN,
		// Off deliberately: the services attach the ids an event needs
		// themselves. SendDefaultPII would add request headers and bodies,
		// which on this backend means Authorization headers, mailbox
		// credentials on the connect path, and message content.
		SendDefaultPII: false,
		Environment:    cfg.Environment,
		Release:        cfg.Release,
		ServerName:     cfg.Service,
	})
	if err != nil {
		return nil, fmt.Errorf("init sentry: %w", err)
	}
	return sentrySink{}, nil
}

func (sentrySink) capture(ev event) {
	hub := sentryHub(ev.ctx)
	// The scope is pushed for this event only, so one caller's tags never leak
	// onto the next event reported on the same hub.
	hub.WithScope(func(scope *sentry.Scope) {
		for k, v := range ev.scope.tags {
			scope.SetTag(k, v)
		}
		for k, v := range ev.scope.extra {
			scope.SetExtra(k, v)
		}
		if ev.err != nil {
			hub.CaptureException(redact(ev.err))
			return
		}
		hub.CaptureMessage(ev.message)
	})
}

func (sentrySink) recoverPanic(ctx context.Context, r any, sc scope) {
	hub := sentryHub(ctx)
	hub.WithScope(func(scope *sentry.Scope) {
		for k, v := range sc.tags {
			scope.SetTag(k, v)
		}
		for k, v := range sc.extra {
			scope.SetExtra(k, v)
		}
		hub.RecoverWithContext(ctx, r)
	})
}

func (sentrySink) flush(timeout time.Duration) bool {
	return sentry.Flush(timeout)
}

// sentryHub returns the hub bound to ctx, or the process-wide one. A request
// scoped hub is what carries a request's breadcrumbs onto its events.
func sentryHub(ctx context.Context) *sentry.Hub {
	if ctx != nil {
		if hub := sentry.GetHubFromContext(ctx); hub != nil {
			return hub
		}
	}
	return sentry.CurrentHub()
}
