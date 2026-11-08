package errs

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/posthog/posthog-go"
)

// posthogSink reports to PostHog's error tracking, which is the default backend
// for a Warmbly instance: the project key an operator may already have set for
// product analytics is the same key error events are captured with, so there is
// one vendor and one place to look instead of two.
//
// Nothing here identifies a person. The distinct id is the service name, not a
// user, and person profiles are switched off per event, so an exception never
// creates or updates a profile for anybody.
type posthogSink struct {
	client     posthog.Client
	distinctID string
	// base is the service, environment and release every event carries.
	base posthog.Properties
	// stacks resolves the Go stack into PostHog frames. Held rather than
	// constructed per event so the in-app rule is decided once.
	stacks posthog.DefaultStackTraceExtractor
	// logger is the SDK's, reused here so a failing backend costs one log line
	// per process rather than one per event.
	logger *posthogLogger
	closed sync.Once
}

// stackSkip drops runtime.Callers, the extractor, the sink method and this
// package's two fan-out frames, so the top frame of a reported stack is the
// line that called CaptureException. Every public entry point is the same
// depth from here, which is why one constant covers them all.
const stackSkip = 5

func newPostHogSink(cfg Config) (sink, error) {
	disableGeoIP := true
	logger := &posthogLogger{}
	client, err := posthog.NewWithConfig(cfg.PostHogKey, posthog.Config{
		// Empty means the SDK's own default, PostHog Cloud US.
		Endpoint: cfg.PostHogHost,
		// A server's IP is the datacentre's, so geolocating it says nothing
		// and storing it is worse than useless.
		DisableGeoIP: &disableGeoIP,
		// Close waits forever without this, so an unreachable host would hang
		// the exit path that flushes rather than delay it.
		ShutdownTimeout: fatalFlushTimeout,
		Logger:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("init posthog error tracking: %w", err)
	}

	base := posthog.Properties{"service": cfg.Service}
	if cfg.Environment != "" {
		base["environment"] = cfg.Environment
	}
	if cfg.Release != "" {
		base["release"] = cfg.Release
	}

	return &posthogSink{
		client:     client,
		distinctID: distinctID(cfg.Service),
		base:       base,
		stacks:     posthog.DefaultStackTraceExtractor{InAppDecider: posthog.SimpleInAppDecider},
		logger:     logger,
	}, nil
}

func (p *posthogSink) capture(ev event) {
	stack := p.stacks.GetStackTrace(stackSkip)
	handled := true

	if ev.err != nil {
		err := redact(ev.err)
		p.enqueue(ev.scope, "error", posthog.ExceptionItem{
			// The Go type is the issue title, the message its description.
			Type:       exceptionType(err),
			Value:      err.Error(),
			Mechanism:  &posthog.ExceptionMechanism{Handled: &handled},
			Stacktrace: stack,
		})
		return
	}

	p.enqueue(ev.scope, "info", posthog.ExceptionItem{
		// A fixed title, because a message's text is the description and
		// putting it in the title would make one issue per wording.
		Type:       "message",
		Value:      ev.message,
		Mechanism:  &posthog.ExceptionMechanism{Handled: &handled},
		Stacktrace: stack,
	})
}

func (p *posthogSink) recoverPanic(_ context.Context, r any, sc scope) {
	stack := p.stacks.GetStackTrace(stackSkip)
	handled := false

	p.enqueue(sc, "error", posthog.ExceptionItem{
		Type:       "panic",
		Value:      fmt.Sprint(r),
		Mechanism:  &posthog.ExceptionMechanism{Handled: &handled},
		Stacktrace: stack,
	})
}

// flush closes the client, which is the only flush the SDK offers. Callers are
// on their way out of the process, which is why that is acceptable here and why
// Flush is documented as an exit-path call.
func (p *posthogSink) flush(time.Duration) bool {
	drained := false
	p.closed.Do(func() {
		drained = p.client.Close() == nil
	})
	return drained
}

func (p *posthogSink) enqueue(sc scope, level string, item posthog.ExceptionItem) {
	props := posthog.Properties{
		// No person is created or updated by an exception: the distinct id
		// names a process, not somebody.
		"$process_person_profile": false,
		"$exception_level":        level,
	}
	for k, v := range p.base {
		props[k] = v
	}
	for k, v := range sc.tags {
		props[k] = v
	}
	for k, v := range sc.extra {
		props[k] = v
	}

	err := p.client.Enqueue(posthog.Exception{
		DistinctId:    p.distinctID,
		Timestamp:     time.Now().UTC(),
		Properties:    props,
		ExceptionList: []posthog.ExceptionItem{item},
	})
	if err != nil {
		p.logger.Errorf("cannot queue an exception: %v", err)
	}
}

// distinctID names the process rather than a person. PostHog requires one on
// every event, so it gets the least identifying value that still separates the
// backend's issues from a worker's.
func distinctID(service string) string {
	if service == "" {
		return "warmbly"
	}
	return "warmbly-" + service
}

// posthogLogger keeps the SDK's own chatter out of the instance's logs and
// bounds its failures to one line per process.
//
// A wrong key or an unreachable host fails on every single batch, so logging
// each one would bury the real logs under reporting noise. Logging none of them
// is worse: a misconfigured key would look exactly like a quiet week.
type posthogLogger struct {
	warned sync.Once
}

func (l *posthogLogger) Debugf(string, ...any) {}
func (l *posthogLogger) Logf(string, ...any)   {}

func (l *posthogLogger) Warnf(format string, args ...any) {
	l.Errorf(format, args...)
}

func (l *posthogLogger) Errorf(format string, args ...any) {
	l.warned.Do(func() {
		log.Printf("error reporting to PostHog is failing for this run (check POSTHOG_KEY and POSTHOG_HOST): "+format, args...)
	})
}
