package smtp

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

// A server limiting connections from one client address refuses every mailbox
// it hosts at once, so the worker backs off that server as a whole.
const (
	paceCooldownBase = 30 * time.Second
	paceCooldownMax  = 10 * time.Minute
	pacedWindow      = 30 * time.Minute
	pacedSpacing     = 3 * time.Second
	paceMaxWait      = 10 * time.Second
	// PaceStage marks a send held on the worker without contacting the server.
	PaceStage = "pacing"
)

// hostPace is one worker address's standing with one SMTP server.
type hostPace struct {
	mu         sync.Mutex
	coolUntil  time.Time
	pacedUntil time.Time
	nextStart  time.Time
	strikes    int
}

var hostPaces sync.Map

func paceFor(host string, port int, local *net.TCPAddr) *hostPace {
	key := net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port)) + "|"
	if local != nil {
		key += local.IP.String()
	}
	v, _ := hostPaces.LoadOrStore(key, &hostPace{})
	return v.(*hostPace)
}

// admit reserves a session start, or reports when the server may be tried again.
func (p *hostPace) admit(now time.Time) (wait time.Duration, retryAt time.Time, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.Before(p.coolUntil) {
		return 0, p.coolUntil, false
	}
	if !now.Before(p.pacedUntil) {
		return 0, time.Time{}, true
	}
	start := p.nextStart
	if start.Before(now) {
		start = now
	}
	if start.Sub(now) > paceMaxWait {
		return 0, start, false
	}
	p.nextStart = start.Add(pacedSpacing)
	return start.Sub(now), time.Time{}, true
}

// wait holds the caller until its reserved start; a refusal is returned as a send that never left.
func (p *hostPace) wait(ctx context.Context, now time.Time) *errx.MailError {
	wait, retryAt, ok := p.admit(now)
	if !ok {
		return pacedHold(now, retryAt)
	}
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return pacedHold(now, now.Add(wait))
	}
}

// observe records a session's outcome; one burst of refusals counts as one strike.
func (p *hostPace) observe(now time.Time, err *errx.MailError) *errx.MailError {
	throttled := serverThrottled(err)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !throttled {
		if err == nil && !now.Before(p.coolUntil) {
			p.strikes = 0
		}
		return err
	}
	if !now.Before(p.coolUntil) {
		p.strikes++
		cool := paceCooldownBase << min(p.strikes-1, 5)
		if cool > paceCooldownMax {
			cool = paceCooldownMax
		}
		p.coolUntil = now.Add(cool)
	}
	p.pacedUntil = now.Add(pacedWindow)
	return throttledFailure(err, p.coolUntil)
}

// serverThrottled is a refusal of this worker's address rather than of the mailbox.
func serverThrottled(err *errx.MailError) bool {
	if err == nil || err.Failure == nil {
		return false
	}
	f := err.Failure
	switch {
	case f.Status == 421:
		return true
	case f.Stage == "auth" && f.Status == 454:
		return true
	case f.Stage == "starttls" && f.Status == 0:
		// A dropped handshake; a refused certificate or a port without STARTTLS carries its own reason.
		return strings.HasPrefix(err.Message, errx.ErrMailServerUnreachable.Message)
	}
	return false
}

func throttledFailure(err *errx.MailError, retryAt time.Time) *errx.MailError {
	out := *err
	f := *err.Failure
	f.RetryAt = &retryAt
	switch f.Stage {
	case "greeting", "ehlo", "starttls", "auth", "mail from":
		// Refused before any recipient was offered, so the send costs the lead nothing.
		f.Cause = errx.SendCauseServerPacing
	}
	out.Failure = &f
	out.RetryAfter = retryAt.Sub(f.ObservedAt)
	out.Message = errx.MailServerThrottlingMessage + strings.TrimPrefix(err.Message, errx.ErrMailServerUnreachable.Message)
	return &out
}

func pacedHold(now, retryAt time.Time) *errx.MailError {
	return errx.WithSendFailure(errx.MError(errx.MailErrorWarning, errx.MailErrorCodeServerUnreachable, errx.MailServerPacedMessage, errx.MailErrorResolveMethodRetry),
		errx.SendFailure{Provider: "smtp", Protocol: "smtp", Stage: PaceStage, Cause: errx.SendCauseServerPacing, Scope: "mailbox", Disposition: errx.SendRetry, ObservedAt: now.UTC(), RetryAt: &retryAt})
}
