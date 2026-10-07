package smtp

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// fakeServer is a hand-rolled SMTP server, because the mechanisms that matter
// here are the ones a real server chooses to advertise and net/smtp ships no
// server to configure. It speaks only enough to get a message accepted.
type fakeServer struct {
	ln net.Listener
	// authAdvertised is the AUTH parameter list, e.g. "LOGIN" or "PLAIN LOGIN".
	// Empty advertises no AUTH extension at all.
	authAdvertised string
	// reject is the reply given to the command whose verb it is keyed by
	// ("MAIL", "RCPT", "AUTH", "DATA-END"). Fixed before serve starts: a
	// write afterwards races the session goroutine, and dial-then-accept is
	// not a happens-before edge the race detector recognises.
	reject map[string]string

	mu sync.Mutex
	// seen records the commands the client actually sent, so a test can prove
	// which mechanism was chosen rather than only that the send succeeded.
	seen []string
	// credentials are what the client supplied, decoded.
	credentials []string
}

func newFakeServer(t *testing.T, authAdvertised string) *fakeServer {
	return newRejectingServer(t, authAdvertised, nil)
}

// newRejectingServer is newFakeServer with a canned refusal for one command.
func newRejectingServer(t *testing.T, authAdvertised string, reject map[string]string) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if reject == nil {
		reject = map[string]string{}
	}
	s := &fakeServer{ln: ln, authAdvertised: authAdvertised, reject: reject}
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *fakeServer) addr() (string, int) {
	host, port, _ := net.SplitHostPort(s.ln.Addr().String())
	p := 0
	for _, r := range port {
		p = p*10 + int(r-'0')
	}
	return host, p
}

func (s *fakeServer) record(cmd string) {
	s.mu.Lock()
	s.seen = append(s.seen, cmd)
	s.mu.Unlock()
}

func (s *fakeServer) sawPrefix(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.seen {
		if strings.HasPrefix(strings.ToUpper(c), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

func (s *fakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

func (s *fakeServer) session(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	w := func(format string, a ...any) { _, _ = fmt.Fprintf(conn, format+"\r\n", a...) }
	br := bufio.NewReader(conn)

	w("220 fake ESMTP")
	inData := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				if reply, bad := s.reject["DATA-END"]; bad {
					w("%s", reply)
					continue
				}
				w("250 2.0.0 accepted")
			}
			continue
		}

		s.record(line)
		verb := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(verb, "EHLO"):
			w("250-fake greets you")
			if s.authAdvertised != "" {
				w("250-AUTH %s", s.authAdvertised)
			}
			w("250 8BITMIME")
		case strings.HasPrefix(verb, "HELO"):
			w("250 fake")
		case strings.HasPrefix(verb, "AUTH LOGIN"):
			if reply, bad := s.reject["AUTH"]; bad {
				w("%s", reply)
				continue
			}
			w("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
			user, _ := br.ReadString('\n')
			s.decode(strings.TrimSpace(user))
			w("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
			pass, _ := br.ReadString('\n')
			s.decode(strings.TrimSpace(pass))
			w("235 2.7.0 authenticated")
		case strings.HasPrefix(verb, "AUTH PLAIN"):
			if reply, bad := s.reject["AUTH"]; bad {
				w("%s", reply)
				continue
			}
			w("235 2.7.0 authenticated")
		case strings.HasPrefix(verb, "AUTH"):
			// A mechanism this server does not implement.
			w("504 5.5.4 unrecognized authentication type")
		case strings.HasPrefix(verb, "MAIL"):
			if reply, bad := s.reject["MAIL"]; bad {
				w("%s", reply)
				continue
			}
			w("250 2.1.0 ok")
		case strings.HasPrefix(verb, "RCPT"):
			if reply, bad := s.reject["RCPT"]; bad {
				w("%s", reply)
				continue
			}
			w("250 2.1.5 ok")
		case strings.HasPrefix(verb, "DATA"):
			inData = true
			w("354 go ahead")
		case strings.HasPrefix(verb, "QUIT"):
			w("221 2.0.0 bye")
			return
		case strings.HasPrefix(verb, "RSET"), strings.HasPrefix(verb, "NOOP"):
			w("250 2.0.0 ok")
		default:
			w("500 5.5.1 unrecognized")
		}
	}
}

func (s *fakeServer) decode(b64 string) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.credentials = append(s.credentials, string(raw))
	s.mu.Unlock()
}

// A server that advertises only LOGIN is the case that could not send at all:
// the client sent AUTH PLAIN blind, the server refused, and the refusal was
// reported to the mailbox's owner as a wrong password.
func TestNegotiateAuthPrefersWhatTheServerAdvertises(t *testing.T) {
	for _, tc := range []struct {
		advertised string
		wantCmd    string
	}{
		{"LOGIN", "AUTH LOGIN"},
		{"PLAIN", "AUTH PLAIN"},
		// Both offered: LOGIN, because a server that speaks only one of the
		// two speaks LOGIN, and preferring it costs nothing here.
		{"PLAIN LOGIN", "AUTH LOGIN"},
		{"CRAM-MD5 PLAIN LOGIN", "AUTH CRAM-MD5"},
	} {
		srv := newFakeServer(t, tc.advertised)
		host, port := srv.addr()
		c := newTestClient(host, port)

		err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
		if tc.advertised == "CRAM-MD5 PLAIN LOGIN" {
			// The fake does not implement the CRAM-MD5 exchange; what matters
			// is that the client asked for it.
			if !srv.sawPrefix(tc.wantCmd) {
				t.Errorf("advertised %q: client never sent %q", tc.advertised, tc.wantCmd)
			}
			continue
		}
		if err != nil {
			t.Errorf("advertised %q: send failed: %v", tc.advertised, err.Message)
		}
		if !srv.sawPrefix(tc.wantCmd) {
			t.Errorf("advertised %q: client never sent %q, sent %v", tc.advertised, tc.wantCmd, srv.seen)
		}
	}
}

// The LOGIN exchange has to hand over the real credentials, not just pick the
// mechanism.
func TestLoginAuthSendsTheCredentials(t *testing.T) {
	srv := newFakeServer(t, "LOGIN")
	host, port := srv.addr()
	c := newTestClient(host, port)

	if err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("send: %v", err.Message)
	}
	srv.mu.Lock()
	got := append([]string(nil), srv.credentials...)
	srv.mu.Unlock()
	if len(got) != 2 || got[0] != "user@warmbly.test" || got[1] != "hunter2" {
		t.Fatalf("server received %q, want the username then the password", got)
	}
}

// A server advertising no AUTH wants none. Refusing to send would break the
// local development sink and any relay that authorizes by IP.
func TestSendWithoutAuthExtension(t *testing.T) {
	srv := newFakeServer(t, "")
	host, port := srv.addr()
	c := newTestClient(host, port)

	if err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("send to a server with no AUTH: %v", err.Message)
	}
	if srv.sawPrefix("AUTH") {
		t.Error("client authenticated against a server that advertises no AUTH")
	}
}

// The sender's own domain goes in EHLO. net/smtp says "localhost" when left
// alone, which relays read as a spam signal.
func TestEHLOAnnouncesTheSenderDomain(t *testing.T) {
	srv := newFakeServer(t, "LOGIN")
	host, port := srv.addr()
	c := newTestClient(host, port)

	_ = c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
	if !srv.sawPrefix("EHLO warmbly.test") {
		t.Errorf("EHLO did not announce the sender domain: %v", srv.seen)
	}
}

// A 5xx is the server's final answer. Retrying it cannot deliver the message
// and spends the mailbox's daily budget, so it must be distinguishable from
// an outage, which is what every one of these used to be reported as.
func TestPermanentRefusalsAreNotReportedAsAnOutage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		at       string
		reply    string
		wantCode string
	}{
		{"sender blocked", "MAIL", "550 5.7.1 sender denied", "SEND_REJECTED"},
		{"recipient unknown", "RCPT", "550 5.1.1 no such user", "RECIPIENT_REJECTED"},
		{"message refused", "DATA-END", "554 5.7.1 message rejected", "SEND_REJECTED"},
		// A 4xx invites a retry, so it stays an outage-shaped error.
		{"sender throttled", "MAIL", "451 4.7.1 try again later", "SERVER_UNREACHABLE"},
		{"recipient greylisted", "RCPT", "450 4.2.0 greylisted", "SERVER_UNREACHABLE"},
		{"message deferred", "DATA-END", "451 4.3.0 try later", "SERVER_UNREACHABLE"},
	} {
		srv := newRejectingServer(t, "LOGIN", map[string]string{tc.at: tc.reply})
		host, port := srv.addr()
		c := newTestClient(host, port)

		err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
		if err == nil {
			t.Errorf("%s: send succeeded against %q", tc.name, tc.reply)
			continue
		}
		if string(err.Code) != tc.wantCode {
			t.Errorf("%s (%q): code = %q, want %q", tc.name, tc.reply, err.Code, tc.wantCode)
		}
	}
}

// A refused AUTH is only a credentials problem when the server says so with a
// 5xx. A 4xx is the server declining for now, and deactivating the mailbox
// over it tells the owner their password is wrong when it is not.
func TestTransientAuthFailureIsNotACredentialsProblem(t *testing.T) {
	srv := newRejectingServer(t, "LOGIN", map[string]string{"AUTH": "454 4.7.0 temporary authentication failure"})
	host, port := srv.addr()
	c := newTestClient(host, port)

	err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
	if err == nil {
		t.Fatal("send succeeded despite a refused AUTH")
	}
	if string(err.Code) != "SERVER_UNREACHABLE" {
		t.Errorf("code = %q, want the transient classification", err.Code)
	}
}

// A server whose advertised mechanisms we do not implement should say so,
// rather than reporting the password as wrong.
func TestUnsupportedAuthMechanismIsItsOwnError(t *testing.T) {
	srv := newFakeServer(t, "GSSAPI NTLM")
	host, port := srv.addr()
	c := newTestClient(host, port)

	err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
	if err == nil {
		t.Fatal("send succeeded against a server offering no mechanism we speak")
	}
	if string(err.Code) != "AUTH_UNSUPPORTED" {
		t.Errorf("code = %q, want AUTH_UNSUPPORTED", err.Code)
	}
}

// A peer that accepts the connection and then says nothing must not hold the
// send goroutine forever. Only the dial was bounded before, and net/smtp sets
// no deadline of its own, so a dropped NAT mapping parked the send.
func TestSendTimesOutOnASilentPeer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		select {}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port := 0
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}
	c := newTestClient(host, port)

	ctx, cancel := contextWithDeadline(t, 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = c.sendRaw(ctx, "sender@warmbly.test", []string{"to@example.test"}, []byte("body"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a send against a peer that never answers did not return: the goroutine is parked forever")
	}
}

// newTestClient points a client at the in-process fake, which speaks no TLS.
func newTestClient(host string, port int) *Client {
	return &Client{
		FirstName: "Test",
		Email:     "sender@warmbly.test",
		AuthType:  models.AuthPlain,
		Credentials: &models.Service{
			Username: "user@warmbly.test",
			Password: "hunter2",
			Host:     host,
			Port:     port,
			Security: models.MailSecurityStartTLS,
		},
		plaintext: true,
	}
}

func contextWithDeadline(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), d)
}

// Native codes explain refusal without transporting raw provider messages.
func TestRecipientRejectionCarriesTheServersReason(t *testing.T) {
	srv := newRejectingServer(t, "LOGIN", map[string]string{"RCPT": "550 5.1.1 no such user here"})
	host, port := srv.addr()
	c := newTestClient(host, port)

	err := c.sendRaw(t.Context(), "sender@warmbly.test", []string{"to@example.test"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
	if err == nil {
		t.Fatal("send succeeded against a refused recipient")
	}
	if err.Failure == nil || err.Failure.EnhancedStatus != "5.1.1" || err.Failure.Status != 550 || !strings.Contains(err.Message, "5.1.1") || strings.Contains(err.Message, "no such user here") {
		t.Errorf("message=%q failure=%+v, want safe native recipient evidence", err.Message, err.Failure)
	}
	// Named apart from the message, so a refused copy is not read as the lead.
	if err.Recipient != "to@example.test" {
		t.Errorf("recipient = %q, want the refused address", err.Recipient)
	}
}
