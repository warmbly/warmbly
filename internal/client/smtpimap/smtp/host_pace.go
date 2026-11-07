package smtp

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
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
	// causeDroppedHandshake marks a TLS handshake the server closed, as opposed to one it refused with a reason.
	causeDroppedHandshake = "dropped_handshake"
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

// paceAfter is the reserved-start timer, replaceable so a test can order a refusal before it fires.
var paceAfter = time.After

// paceFor keys on the server, not the port, so a 587 fallback shares the 465 standing; a relay on this machine has none.
func paceFor(host string, local *net.TCPAddr) *hostPace {
	host = strings.ToLower(strings.TrimSuffix(models.NormalizeMailHost(host), "."))
	if models.LoopbackMailHost(host) {
		return nil
	}
	key := host + "|"
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
	select {
	case <-paceAfter(wait):
		return p.heldIfCooling(time.Now())
	case <-ctx.Done():
		return pacedHold(now, now.Add(wait))
	}
}

// heldIfCooling refuses a reserved start that a refusal since the reservation put inside a cooldown.
func (p *hostPace) heldIfCooling(now time.Time) *errx.MailError {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.Before(p.coolUntil) {
		return pacedHold(now, p.coolUntil)
	}
	return nil
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
	p.pacedUntil = p.coolUntil.Add(pacedWindow)
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
	case (f.Stage == "starttls" || f.Stage == "dial") && f.Status == 0:
		return f.Cause == causeDroppedHandshake
	}
	return false
}

// droppedConnection is the server closing or resetting the connection, never a TLS alert or a certificate refusal.
func droppedConnection(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}

func throttledFailure(err *errx.MailError, retryAt time.Time) *errx.MailError {
	out := *err
	f := *err.Failure
	f.RetryAt = &retryAt
	limit := f.Cause == errx.SendCauseSendingLimit || f.Cause == errx.SendCauseSenderBlocked
	if !limit {
		f.Cause = ""
	}
	switch f.Stage {
	case "dial", "greeting", "ehlo", "starttls", "auth", "mail from":
		// Refused before any recipient was offered, so the send costs the lead nothing.
		if !limit {
			f.Cause = errx.SendCauseServerPacing
		}
	}
	out.Failure = &f
	out.RetryAfter = retryAt.Sub(f.ObservedAt)
	detail := strings.TrimPrefix(err.Message, errx.ErrMailServerUnreachable.Message)
	if detail == err.Message {
		detail = " " + detail
	}
	out.Message = errx.MailServerThrottlingMessage + detail
	return &out
}

func pacedHold(now, retryAt time.Time) *errx.MailError {
	return errx.WithSendFailure(errx.MError(errx.MailErrorWarning, errx.MailErrorCodeServerUnreachable, errx.MailServerPacedMessage, errx.MailErrorResolveMethodRetry),
		errx.SendFailure{Provider: "smtp", Protocol: "smtp", Stage: PaceStage, Cause: errx.SendCauseServerPacing, Scope: "mailbox", Disposition: errx.SendRetry, ObservedAt: now.UTC(), RetryAt: &retryAt})
}
