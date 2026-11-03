package smtp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/textproto"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func throttleReply(stage string, status int) *errx.MailError {
	var native error = io.EOF
	if status != 0 {
		native = &textproto.Error{Code: status, Msg: "4.7.0 too many connections from your IP"}
	}
	return sendFailure(errx.ErrMailServerUnreachable, native, stage, "mailbox")
}

func starttlsDrop(err error) *errx.MailError {
	return handshakeFailure(errx.ErrMailServerUnreachable, "starttls", err)
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
		{"dropped handshake", starttlsDrop(io.EOF), true},
		{"reset handshake", starttlsDrop(&net.OpError{Op: "read", Err: syscall.ECONNRESET}), true},
		{"dropped implicit TLS", handshakeFailure(errx.ErrMailServerUnreachableAt("dial smtp.example.test:465", io.EOF), "dial", io.EOF), true},
		{"refused dial", sendFailure(errx.ErrMailServerUnreachableAt("dial smtp.example.test:465", syscall.ECONNREFUSED), &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "dial", "mailbox"), false},
		{"handshake alert", starttlsDrop(errors.New("remote error: tls: protocol version not supported")), false},
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

	first := p.observe(t0, starttlsDrop(io.EOF))
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
	last := p.coolUntil.Add(-paceCooldownMax)
	p.observe(p.coolUntil, starttlsDrop(io.EOF))
	if got := p.coolUntil.Sub(last.Add(paceCooldownMax)); got != paceCooldownMax {
		t.Fatalf("cooldown not capped: %v", got)
	}
	if !p.pacedUntil.Equal(p.coolUntil.Add(pacedWindow)) {
		t.Fatalf("spacing not measured from the end of the cooldown: paced=%v cool=%v", p.pacedUntil, p.coolUntil)
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
	p.observe(t0, starttlsDrop(io.EOF))
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
	a := paceFor("SMTP.Example.test", &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1)})
	if paceFor("smtp.example.test.", &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1)}) != a {
		t.Fatal("host spelling split the pace")
	}
	if paceFor("smtp.example.test", &net.TCPAddr{IP: net.IPv4(192, 0, 2, 2)}) == a || paceFor("smtp.example.test", nil) == a {
		t.Fatal("another worker address shared the pace")
	}
	if paceFor("::1", nil) != nil || paceFor("localhost", nil) != nil || paceFor("LocalHost.", nil) != nil {
		t.Fatal("a relay on this machine was paced")
	}
}

func TestHostPaceReservationYieldsToALaterCooldown(t *testing.T) {
	fire := make(chan time.Time)
	reserved := make(chan struct{})
	paceAfter = func(time.Duration) <-chan time.Time { close(reserved); return fire }
	t.Cleanup(func() { paceAfter = time.After })
	p := &hostPace{}
	now := time.Now()
	p.pacedUntil = now.Add(pacedWindow)
	p.nextStart = now.Add(time.Second)
	done := make(chan *errx.MailError, 1)
	go func() { done <- p.wait(context.Background(), now) }()
	<-reserved
	p.mu.Lock()
	p.coolUntil = time.Now().Add(time.Minute)
	cool := p.coolUntil
	p.mu.Unlock()
	fire <- time.Now()
	held := <-done
	if held == nil || held.Failure.Stage != PaceStage || !held.Failure.RetryAt.Equal(cool) {
		t.Fatalf("reserved start ran inside a cooldown: %+v", held)
	}
}

func TestSendRawMarksAnImplicitTLSHandshakeTheServerClosed(t *testing.T) {
	ln, err := net.Listen("tcp", net.JoinHostPort("::1", "0"))
	if err != nil {
		ln, err = net.Listen("tcp", net.JoinHostPort("localhost", "0"))
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	c := newTestClient(addr.IP.String(), addr.Port)
	c.Credentials.Security = models.MailSecurityTLS
	got := c.sendRaw(t.Context(), "a@example.test", []string{"b@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
	if got == nil || got.Failure == nil || got.Failure.Stage != "dial" || !serverThrottled(got) {
		t.Fatalf("closed implicit-TLS handshake not read as the server shedding connections: %+v", got)
	}
}
