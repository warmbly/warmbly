// Package errs is the only place an error-reporting SDK is called from.
//
// Every service, job and repository reports through these functions, so the
// vendor imports stay in two files: turning reporting off, attaching a tag to
// every event, or adding a backend is a change here rather than across ninety
// files. Nothing here needs Init to have run; with no backend configured an
// event is summarised to the local log, which is what a deployment that
// configured no reporting wants.
//
// Two backends exist and either, both or neither can be on:
//
//   - PostHog (POSTHOG_KEY), the default. Error tracking sits in the same
//     project as the product analytics this instance may already send, so one
//     key covers both and there is no second vendor to sign up with.
//   - Sentry (SENTRY_DSN), kept because it is what an operator who already runs
//     Sentry wants and because dropping a working integration to switch vendors
//     is not a migration anybody asked for.
package errs

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"
)

// Config is what a service knows about itself at boot.
type Config struct {
	// PostHogKey is the project API key error events are captured with. Empty
	// turns the PostHog backend off.
	PostHogKey string
	// PostHogHost is the capture host. Empty means PostHog Cloud US.
	PostHogHost string
	// SentryDSN is empty when the operator configured no Sentry project.
	SentryDSN string
	// Environment is the deployment label (dev, staging, prod).
	Environment string
	// Release identifies the build, so a stack trace can be tied to a commit.
	Release string
	// Service names the process (backend, consumer, worker, forms).
	Service string
}

// sink is one configured backend. Everything public in this package fans out
// over the enabled sinks, so a service reporting to both pays one call.
type sink interface {
	capture(event)
	recoverPanic(ctx context.Context, r any, sc scope)
	flush(timeout time.Duration) bool
}

// active holds the sinks Init built. A pointer swap rather than a plain slice
// so a capture racing a late Init reads one state or the other.
var active atomic.Pointer[[]sink]

// Init configures the SDKs for one process. Safe to call once per process.
//
// A backend is used when it is configured, in any environment. Error reporting
// is the operator's choice, not a requirement of the software: demanding an
// account to run APP_ENV=prod made a self-hosted deployment fail to boot over a
// service it never asked for.
func Init(cfg Config) error {
	var sinks []sink
	var initErr error

	if cfg.PostHogKey != "" {
		s, err := newPostHogSink(cfg)
		if err != nil {
			initErr = err
		} else {
			sinks = append(sinks, s)
		}
	}
	if cfg.SentryDSN != "" {
		s, err := newSentrySink(cfg)
		if err != nil {
			initErr = err
		} else {
			sinks = append(sinks, s)
		}
	}

	if len(sinks) == 0 {
		if cfg.Environment == "prod" {
			log.Printf("Error reporting is not configured (no POSTHOG_KEY, no SENTRY_DSN); errors are logged locally only.")
		}
		sinks = append(sinks, localSink{service: cfg.Service})
	}

	active.Store(&sinks)
	return initErr
}

// Option decorates the scope one event is reported on. Callers build these with
// Tag and Extra so they never name a vendor type themselves.
type Option func(*scope)

// scope is the per-event detail, in a shape both backends can render: Sentry
// splits it into tags and extras, PostHog flattens the pair into the event's
// properties.
type scope struct {
	tags  map[string]string
	extra map[string]any
}

// Tag adds an indexed key/value pair, searchable and groupable.
func Tag(key, value string) Option {
	return func(s *scope) {
		if s.tags == nil {
			s.tags = map[string]string{}
		}
		s.tags[key] = value
	}
}

// Extra adds an unindexed value, for detail that is worth reading on the event
// but not worth searching by.
func Extra(key string, value any) Option {
	return func(s *scope) {
		if s.extra == nil {
			s.extra = map[string]any{}
		}
		s.extra[key] = value
	}
}

// event is one report on its way to every enabled backend.
type event struct {
	ctx context.Context
	// err is set for an exception, message for a plain message. Never both.
	err     error
	message string
	scope   scope
}

// CaptureException reports err. A nil error is dropped: a caller that reports
// unconditionally should not mint an issue with nothing in it.
func CaptureException(err error, opts ...Option) {
	if err == nil || abandoned(err) {
		return
	}
	report(event{err: err, scope: build(opts)})
}

// CaptureExceptionContext reports err on the reporting state carried by ctx
// when there is one, so a request's scope travels with the event.
func CaptureExceptionContext(ctx context.Context, err error, opts ...Option) {
	if err == nil || abandoned(err) {
		return
	}
	report(event{ctx: ctx, err: err, scope: build(opts)})
}

// abandoned reports whether err says the caller stopped waiting, rather than
// that anything went wrong. A cancelled context is a browser navigating away, a
// client hanging up or a container draining on deploy: the work was dropped on
// purpose, and every layer it unwound through reported the same non-event, so a
// rolling restart filed a handful of issues naming whichever queries happened to
// be in flight.
//
// Dropped here rather than at each call site because the caller cannot tell:
// a repository reporting a failed query has no idea whether the request behind
// it still exists. context.DeadlineExceeded is deliberately not included; a
// deadline this process set and then blew through is its own problem.
func abandoned(err error) bool {
	return errors.Is(err, context.Canceled)
}

// CaptureMessage reports a message with no error attached.
func CaptureMessage(message string, opts ...Option) {
	if message == "" {
		return
	}
	report(event{message: message, scope: build(opts)})
}

// CaptureMessageContext is CaptureMessage on ctx's reporting state.
func CaptureMessageContext(ctx context.Context, message string, opts ...Option) {
	if message == "" {
		return
	}
	report(event{ctx: ctx, message: message, scope: build(opts)})
}

// fatalFlushTimeout is how long a process about to exit waits for its last
// event. Short enough not to stall a crash loop, long enough for one POST.
const fatalFlushTimeout = 2 * time.Second

// CaptureFatal reports err and waits for it to be sent. Use it instead of
// CaptureException wherever the next statement ends the process: both SDKs send
// in the background and os.Exit does not wait for them, so a boot failure, the
// error most worth having, was the one that never arrived.
func CaptureFatal(err error, opts ...Option) {
	CaptureException(err, opts...)
	Flush(fatalFlushTimeout)
}

// Recover reports a value from recover(). Call it inside the deferred function
// that recovered, not after.
func Recover(r any, opts ...Option) {
	reportPanic(context.Background(), r, build(opts))
}

// RecoverContext is Recover on ctx's reporting state.
func RecoverContext(ctx context.Context, r any, opts ...Option) {
	reportPanic(ctx, r, build(opts))
}

// Flush waits up to timeout for queued events to reach their backend, and
// reports whether every queue drained. Worth calling before a process exits on
// purpose: the SDKs send in the background, so an immediate exit loses the last
// event.
//
// It is an exit-path call. The PostHog client has no flush that leaves it
// usable, so flushing it closes it and anything reported afterwards is dropped.
func Flush(timeout time.Duration) bool {
	drained := true
	for _, s := range sinks() {
		if !s.flush(timeout) {
			drained = false
		}
	}
	return drained
}

// report and reportPanic are one frame deep on purpose: the PostHog sink walks
// a fixed number of frames off the stack it captures, so both public entry
// points have to reach a sink through the same depth. See stackSkip.
func report(ev event) {
	for _, s := range sinks() {
		s.capture(ev)
	}
}

func reportPanic(ctx context.Context, r any, sc scope) {
	if r == nil {
		return
	}
	for _, s := range sinks() {
		s.recoverPanic(ctx, r, sc)
	}
}

func sinks() []sink {
	if p := active.Load(); p != nil {
		return *p
	}
	return nil
}

func build(opts []Option) scope {
	var s scope
	for _, opt := range opts {
		opt(&s)
	}
	return s
}
