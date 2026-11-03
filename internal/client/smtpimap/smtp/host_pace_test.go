package smtp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

func throttleReply(stage string, status int) *errx.MailError {
	var native error = io.EOF
	if status != 0 {
		native = &textproto.Error{Code: status, Msg: "4.7.0 too many connections from your IP"}
	}
	return sendFailure(errx.ErrMailServerUnreachable, native, stage, "mailbox")
}

func TestServerThrottledNamesOnlyAddressRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *errx.MailError
		want bool
	}{
		{"greeting 421", throttleReply("greeting", 421), true},
		{"auth 454", throttleReply("auth", 454), true},
		{"rcpt 421", throttleReply("rcpt to", 421), true},
		{"dropped handshake", tlsFailure(errx.ErrMailServerUnreachable, "starttls", io.EOF), true},
		{"refused certificate", sendFailure(errx.ErrMailServerUnreachableAt("starttls", errors.New(errx.MailConnectionReasonCertificate)), io.EOF, "starttls", "mailbox"), false},
		{"no STARTTLS", sendFailure(errx.ErrMailServerUnreachableAt("starttls", errNoSTARTTLS), errNoSTARTTLS, "starttls", "mailbox"), false},
		{"lost auth connection", throttleReply("auth", 0), false},
		{"greylisted recipient", throttleReply("rcpt to", 450), false},
		{"bad password", sendFailure(errx.ErrMailInvalidCredentials, &textproto.Error{Code: 535, Msg: "5.7.8 bad"}, "auth", "mailbox"), false},
		{"success", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverThrottled(tc.err); got != tc.want {
				t.Fatalf("serverThrottled = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestHostPaceCoolsOncePerBurstAndBacksOff(t *testing.T) {
	p := &hostPace{}
	t0 := time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC)

	first := p.observe(t0, throttleReply("starttls", 0))
	if !first.Failure.RetryAt.Equal(t0.Add(paceCooldownBase)) || first.Failure.Cause != errx.SendCauseServerPacing {
		t.Fatalf("first refusal = %+v", first.Failure)
	}
	if !strings.HasPrefix(first.Message, errx.MailServerThrottlingMessage) || !strings.HasSuffix(first.Message, "(SMTP starttls status 0)") {
		t.Fatalf("message = %q", first.Message)
	}
	// The rest of the same burst lands inside the cooldown and does not extend it.
	p.observe(t0.Add(time.Second), throttleReply("auth", 454))
	if p.strikes != 1 || !p.coolUntil.Equal(t0.Add(paceCooldownBase)) {
		t.Fatalf("burst counted twice: strikes=%d cool=%v", p.strikes, p.coolUntil)
	}
	if _, retry, ok := p.admit(t0.Add(10 * time.Second)); ok || !retry.Equal(t0.Add(paceCooldownBase)) {
		t.Fatalf("admitted during cooldown: ok=%t retry=%v", ok, retry)
	}

	t1 := t0.Add(paceCooldownBase)
	p.observe(t1, throttleReply("greeting", 421))
	if p.strikes != 2 || !p.coolUntil.Equal(t1.Add(2*paceCooldownBase)) {
		t.Fatalf("second burst: strikes=%d cool=%v", p.strikes, p.coolUntil)
	}
	for i := 0; i < 10; i++ {
		p.observe(p.coolUntil, throttleReply("greeting", 421))
	}
	if got := p.coolUntil.Sub(p.pacedUntil.Add(-pacedWindow)); got != paceCooldownMax {
		t.Fatalf("cooldown not capped: %v", got)
	}
}

func TestHostPaceSpacesSessionsAfterARefusalUntilItRecovers(t *testing.T) {
	p := &hostPace{}
	t0 := time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC)
	if wait, _, ok := p.admit(t0); !ok || wait != 0 {
		t.Fatalf("fresh host paced: wait=%v ok=%t", wait, ok)
	}
	p.observe(t0, throttleReply("auth", 454))
	open := p.coolUntil
	var waits []time.Duration
	for i := 0; i < 5; i++ {
		wait, _, ok := p.admit(open)
		if !ok {
			waits = append(waits, -1)
			continue
		}
		waits = append(waits, wait)
	}
	want := []time.Duration{0, pacedSpacing, 2 * pacedSpacing, 3 * pacedSpacing, -1}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
	if p.observe(open, nil) != nil || p.strikes != 0 {
		t.Fatalf("success after the cooldown did not reset strikes: %d", p.strikes)
	}
	if wait, _, ok := p.admit(open.Add(pacedWindow)); !ok || wait != 0 {
		t.Fatalf("still paced after the window: wait=%v ok=%t", wait, ok)
	}
}

func TestHostPaceHoldIsARetryThatNeverReachedTheServer(t *testing.T) {
	p := &hostPace{}
	t0 := time.Now()
	p.observe(t0, throttleReply("starttls", 0))
	held := p.wait(context.Background(), t0)
	if held == nil || held.Failure == nil {
		t.Fatal("cooling host admitted a session")
	}
	f := held.Failure
	if f.Stage != PaceStage || f.Disposition != errx.SendRetry || f.Cause != errx.SendCauseServerPacing || f.RetryAt == nil || !f.RetryAt.Equal(p.coolUntil) {
		t.Fatalf("hold = %+v", f)
	}
	if held.Type == errx.MailErrorCritical || held.Code != errx.MailErrorCodeServerUnreachable {
		t.Fatalf("hold type=%s code=%s", held.Type, held.Code)
	}
}

func TestHostPaceRefusalAfterRecipientsCostsTheLeadAnAttempt(t *testing.T) {
	p := &hostPace{}
	if got := p.observe(time.Now(), throttleReply("rcpt to", 421)); got.Failure.Cause != "" {
		t.Fatalf("recipient-stage refusal marked unattempted: %+v", got.Failure)
	}
}

func TestPaceForKeysOnServerAndWorkerAddress(t *testing.T) {
	a := paceFor("SMTP.Example.test", 587, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1)})
	if paceFor("smtp.example.test", 587, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1)}) != a {
		t.Fatal("host case split the pace")
	}
	if paceFor("smtp.example.test", 587, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 2)}) == a || paceFor("smtp.example.test", 587, nil) == a {
		t.Fatal("another worker address shared the pace")
	}
}
