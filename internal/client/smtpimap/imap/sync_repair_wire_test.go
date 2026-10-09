package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func repairWireServer(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
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
			go func() { defer func() { _ = conn.Close() }(); serve(conn) }()
		}
	}()
	return ln.Addr().String()
}

// This fixture advertises global CONDSTORE but selects a NOMODSEQ folder.
func bodyRepairFixture(t *testing.T, bodyReply func(net.Conn, string) bool) (*Client, *wireLog) {
	t.Helper()
	wire := &wireLog{}
	addr := repairWireServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1 CONDSTORE] ready\r\n")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			wire.add(strings.TrimSpace(line))
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return
			}
			tag := fields[0]
			switch strings.ToUpper(fields[1]) {
			case "LOGIN":
				_, _ = fmt.Fprintf(conn, "%s OK [CAPABILITY IMAP4rev1 CONDSTORE] authenticated\r\n", tag)
			case "CAPABILITY":
				_, _ = fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1 CONDSTORE\r\n%s OK capability\r\n", tag)
			case "EXAMINE":
				_, _ = fmt.Fprintf(conn, "* 1 EXISTS\r\n* OK [UIDVALIDITY 7] generation\r\n* OK [UIDNEXT 2] next\r\n* OK [NOMODSEQ] no modseq\r\n%s OK [READ-ONLY] selected\r\n", tag)
			case "UID":
				if len(fields) > 2 && strings.EqualFold(fields[2], "SEARCH") {
					_, _ = fmt.Fprintf(conn, "* SEARCH 1\r\n%s OK search\r\n", tag)
				} else if strings.Contains(strings.ToUpper(line), "BODYSTRUCTURE") {
					_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID 1 FLAGS () ENVELOPE (NIL \"hello\" NIL NIL NIL NIL NIL NIL NIL \"<wire@test>\") BODYSTRUCTURE (\"TEXT\" \"PLAIN\" (\"CHARSET\" \"UTF-8\") NIL NIL \"7BIT\" 13 1))\r\n%s OK envelope\r\n", tag)
				} else if bodyReply != nil {
					if !bodyReply(conn, tag) {
						return
					}
				} else {
					_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID 1 FLAGS (\\Seen) ENVELOPE (NIL NIL NIL NIL NIL NIL NIL NIL NIL \"<wire@test>\"))\r\n%s OK flags\r\n", tag)
				}
			default:
				_, _ = fmt.Fprintf(conn, "%s BAD unexpected command\r\n", tag)
			}
		}
	})
	return clientTo(t, addr), wire
}

func TestFetchBodyWireErrorsRemainRetryable(t *testing.T) {
	for _, failure := range []string{"tagged refusal", "short literal", "reset literal", "missing section", "failed completion"} {
		t.Run(failure, func(t *testing.T) {
			var attempts atomic.Int32
			c, _ := bodyRepairFixture(t, func(conn net.Conn, tag string) bool {
				if attempts.Add(1) == 1 {
					switch failure {
					case "tagged refusal":
						_, _ = fmt.Fprintf(conn, "%s NO [NONEXISTENT] body refused\r\n", tag)
					case "short literal":
						_, _ = io.WriteString(conn, "* 1 FETCH (UID 1 BODY[1] {13}\r\nshort")
						return false
					case "reset literal":
						_, _ = io.WriteString(conn, "* 1 FETCH (UID 1 BODY[1] {13}\r\nshort")
						_ = conn.(*net.TCPConn).SetLinger(0)
						return false
					case "missing section":
						_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID 1)\r\n%s OK completed\r\n", tag)
					case "failed completion":
						_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID 1 BODY[1] {13}\r\ncomplete body)\r\n%s NO incomplete\r\n", tag)
					}
					return true
				}
				_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID 1 BODY[1] {13}\r\ncomplete body)\r\n%s OK body\r\n", tag)
				return true
			})
			c.IdleTimeout = time.Second
			if err := c.Connect(); err != nil {
				t.Fatal(err)
			}
			if _, err := c.SelectForSync("INBOX"); err != nil {
				t.Fatal(err)
			}
			fetched, err := c.FetchEnvelopes(t.Context(), []goimap.UID{1})
			if err != nil || len(fetched) != 1 {
				t.Fatalf("envelope: %v %v", fetched, err)
			}
			if err := c.FetchBody(fetched[0]); err == nil || err.Code == errx.MailErrorCodeNotFound {
				t.Fatalf("hydration failure treated as bodyless or gone: %v", err)
			}
			if fetched[0].Email.BodyPlain != "" || fetched[0].Email.BodyHTML != "" {
				t.Fatal("failed body installed partial text")
			}
			if err := c.ensureConnected(); err != nil {
				t.Fatal(err)
			}
			if _, err := c.SelectForSync("INBOX"); err != nil {
				t.Fatal(err)
			}
			fetched, err = c.FetchEnvelopes(t.Context(), []goimap.UID{1})
			if err != nil || len(fetched) != 1 {
				t.Fatalf("retry envelope: %v %v", fetched, err)
			}
			if err := c.FetchBody(fetched[0]); err != nil {
				t.Fatal(err)
			}
			if fetched[0].Email.BodyPlain != "complete body" {
				t.Fatalf("retry body = %q", fetched[0].Email.BodyPlain)
			}
		})
	}
}

func TestFetchBodyFolderResumeErrorAndValidEmptyBodies(t *testing.T) {
	var refuse atomic.Bool
	c, _ := interceptingServer(t, nil, func(line string) string {
		if refuse.Load() && strings.Contains(strings.ToUpper(line), "EXAMINE") {
			return strings.Fields(line)[0] + " NO folder unavailable"
		}
		return ""
	}, "Archive")
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	uid := putMessage(t, c)
	if _, err := c.SelectForSync("INBOX"); err != nil {
		t.Fatal(err)
	}
	fetched, err := c.FetchEnvelopes(t.Context(), []goimap.UID{uid})
	if err != nil || len(fetched) != 1 {
		t.Fatal("envelope failed")
	}
	if _, err := c.selectMailbox("Archive", nil); err != nil {
		t.Fatal(err)
	}
	refuse.Store(true)
	if err := c.FetchBody(fetched[0]); err == nil {
		t.Fatal("failed folder resume swallowed")
	}
	refuse.Store(false)
	if err := c.FetchBody(fetched[0]); err != nil {
		t.Fatal(err)
	}
	for _, bs := range []goimap.BodyStructure{nil, &goimap.BodyStructureSinglePart{Type: "application", Subtype: "octet-stream"}, &goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Extended: &goimap.BodyStructureSinglePartExt{Disposition: &goimap.BodyStructureDisposition{Value: "attachment"}}}} {
		f := &Fetched{Email: &models.EmailMessageData{}, uid: uid, body: bs}
		if err := c.FetchBody(f); err != nil || f.Email.BodyPlain != "" || f.Email.BodyHTML != "" {
			t.Fatalf("valid no-text message refused: %v", err)
		}
	}
}

func TestNomodseqWireOmitsModseqAndChangedSince(t *testing.T) {
	c, wire := bodyRepairFixture(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	if !c.HasCondStore() {
		t.Fatal("fixture did not advertise CONDSTORE")
	}
	view, err := c.SelectForSyncState("INBOX")
	if err != nil || view.HighestModSeq != 0 {
		t.Fatalf("NOMODSEQ SELECT: %+v %v", view, err)
	}
	if _, err := c.FetchEnvelopes(t.Context(), []goimap.UID{1}); err != nil {
		t.Fatal(err)
	}
	if uids, err := c.SearchNewSince(1); err != nil || len(uids) != 1 {
		t.Fatalf("UIDNEXT fallback: %v %v", uids, err)
	}
	flags, err := c.FetchFlags(t.Context(), 1)
	if err != nil || len(flags) != 1 {
		t.Fatalf("flag fallback: %v %v", flags, err)
	}
	for _, line := range wire.commands("FETCH") {
		if strings.Contains(line, "MODSEQ") || strings.Contains(line, "CHANGEDSINCE") {
			t.Fatalf("NOMODSEQ command: %s", line)
		}
	}
	for _, line := range wire.commands("SEARCH") {
		if strings.Contains(line, "MODSEQ") || strings.Contains(line, "CHANGEDSINCE") {
			t.Fatalf("NOMODSEQ search: %s", line)
		}
	}
}

func TestSetupBlackholesAreBoundedAndCanRedial(t *testing.T) {
	for _, phase := range []string{"greeting", "STARTTLS response", "STARTTLS handshake", "implicit TLS handshake", "authentication"} {
		t.Run(phase, func(t *testing.T) {
			var attempts atomic.Int32
			wire := &wireLog{}
			addr := repairWireServer(t, func(conn net.Conn) {
				attempt := attempts.Add(1)
				if attempt == 1 && (phase == "greeting" || phase == "implicit TLS handshake") {
					_, _ = io.Copy(io.Discard, conn)
					return
				}
				_, _ = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1 STARTTLS] ready\r\n")
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					wire.add(strings.TrimSpace(line))
					fields := strings.Fields(line)
					if len(fields) < 2 {
						return
					}
					tag := fields[0]
					if attempt == 1 {
						if phase == "STARTTLS handshake" {
							_, _ = fmt.Fprintf(conn, "%s OK begin TLS\r\n", tag)
						}
						_, _ = io.Copy(io.Discard, conn)
						return
					}
					switch strings.ToUpper(fields[1]) {
					case "LOGIN":
						_, _ = fmt.Fprintf(conn, "%s OK [CAPABILITY IMAP4rev1] logged in\r\n", tag)
					case "CAPABILITY":
						_, _ = fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1\r\n%s OK capability\r\n", tag)
					default:
						_, _ = fmt.Fprintf(conn, "%s BAD unexpected command\r\n", tag)
					}
				}
			})
			c := clientTo(t, addr)
			c.plaintext = phase == "authentication"
			c.Credentials.Security = models.MailSecurityStartTLS
			if phase == "implicit TLS handshake" {
				c.Credentials.Security = models.MailSecurityTLS
			}
			c.IdleTimeout = 100 * time.Millisecond
			start := time.Now()
			if err := c.Connect(); err == nil {
				t.Fatal("blackhole succeeded")
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("setup budget did not bound silent peer")
			}
			if phase != "authentication" && len(wire.commands("LOGIN")) != 0 {
				t.Fatal("TLS failure fell back to plaintext authentication")
			}
			// The second local connection uses test-only plaintext to isolate redial from certificate trust.
			c.plaintext = true
			if err := c.ensureConnected(); err != nil {
				t.Fatalf("redial: %v", err)
			}
			if attempts.Load() != 2 {
				t.Fatalf("connections = %d", attempts.Load())
			}
		})
	}
}

func TestCloseAndLifetimeCancellationInterruptPendingSetup(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit close=%t", explicit), func(t *testing.T) {
			accepted := make(chan struct{})
			ended := make(chan struct{})
			addr := repairWireServer(t, func(conn net.Conn) { close(accepted); _, _ = io.Copy(io.Discard, conn); close(ended) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := clientTo(t, addr)
			c.plaintext = false
			c.Context = ctx
			c.IdleTimeout = 10 * time.Second
			c.Credentials.Security = models.MailSecurityStartTLS
			result := make(chan *errx.MailError, 1)
			go func() { result <- c.Connect() }()
			select {
			case <-accepted:
			case <-time.After(time.Second):
				t.Fatal("setup did not connect")
			}
			if explicit {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("cancelled setup succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("pending setup did not cancel")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("pending socket did not close")
			}
			if err := c.ensureConnected(); err == nil {
				t.Fatal("retired lifetime redialed")
			}
		})
	}
}

func TestCloseAndLifetimeCancellationCloseIdleSessionWithoutRedial(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit close=%t", explicit), func(t *testing.T) {
			ended := make(chan struct{})
			var attempts atomic.Int32
			addr := repairWireServer(t, func(conn net.Conn) {
				attempts.Add(1)
				_, _ = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						close(ended)
						return
					}
					_, _ = fmt.Fprintf(conn, "%s OK [CAPABILITY IMAP4rev1] authenticated\r\n", strings.Fields(line)[0])
				}
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := clientTo(t, addr)
			c.Context = ctx
			if err := c.Connect(); err != nil {
				t.Fatal(err)
			}
			if explicit {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("idle server did not observe socket EOF")
			}
			if err := c.ensureConnected(); err == nil || attempts.Load() != 1 {
				t.Fatal("retired installed session reconnected")
			}
		})
	}
}

func TestStartTLSSetupStillRejectsUntrustedCertificate(t *testing.T) {
	t.Setenv("MAIL_TLS_INSECURE", "false")
	certServer := httptest.NewTLSServer(nil)
	defer certServer.Close()
	wire := &wireLog{}
	addr := repairWireServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1 STARTTLS] ready\r\n")
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return
		}
		wire.add(strings.TrimSpace(line))
		_, _ = fmt.Fprintf(conn, "%s OK begin TLS\r\n", strings.Fields(line)[0])
		secure := tls.Server(conn, certServer.TLS)
		if err := secure.Handshake(); err == nil {
			line, _ := bufio.NewReader(secure).ReadString('\n')
			wire.add(strings.TrimSpace(line))
		}
	})
	c := clientTo(t, addr)
	c.plaintext = false
	c.Credentials.Security = models.MailSecurityStartTLS
	c.IdleTimeout = time.Second
	if err := c.Connect(); err == nil {
		t.Fatal("untrusted fixture certificate was accepted")
	}
	if len(wire.commands("LOGIN")) != 0 {
		t.Fatal("certificate failure sent plaintext credentials")
	}
}

func TestFetchBodyTLSLiteralFailureReleasesDecoderAndRetries(t *testing.T) {
	var attempts atomic.Int32
	c, _ := bodyRepairFixture(t, func(conn net.Conn, tag string) bool {
		if attempts.Add(1) == 1 {
			_, _ = io.WriteString(conn, "* 1 FETCH (UID 1 BODY[1] {13}\r\nshort")
			return false
		}
		_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID 1 BODY[1] {13}\r\ncomplete body)\r\n%s OK body\r\n", tag)
		return true
	})
	certServer := httptest.NewTLSServer(nil)
	defer certServer.Close()
	backendAddr := net.JoinHostPort(c.Credentials.Host, fmt.Sprint(c.Credentials.Port))
	addr := repairWireServer(t, func(raw net.Conn) {
		secure := tls.Server(raw, certServer.TLS)
		upstream, err := net.DialTimeout("tcp", backendAddr, time.Second)
		if err != nil {
			return
		}
		defer func() { _ = upstream.Close() }()
		go func() { _, _ = io.Copy(upstream, secure); _ = upstream.Close() }()
		_, _ = io.Copy(secure, upstream)
		// Close the raw transport without TLS close_notify to produce TLS unexpected EOF.
	})
	roots := x509.NewCertPool()
	roots.AddCert(certServer.Certificate())
	connect := func() {
		raw, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn := &idleConn{Conn: raw, timeout: time.Second}
		secure := tls.Client(conn, &tls.Config{RootCAs: roots, ServerName: certServer.Certificate().DNSNames[0], MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(t.Context()); err != nil {
			_ = raw.Close()
			t.Fatal(err)
		}
		c.client = imapclient.New(secure, nil)
		c.conn = conn
		c.transport = raw
		c.selected.Store(false)
		c.selection.Store(nil)
		if err := c.plainAuth(); err != nil {
			t.Fatal(err)
		}
		c.condStore.Store(c.client.Caps().Has(goimap.CapCondStore))
		if _, err := c.SelectForSync("INBOX"); err != nil {
			t.Fatal(err)
		}
	}
	connect()
	fetched, err := c.FetchEnvelopes(t.Context(), []goimap.UID{1})
	if err != nil || len(fetched) != 1 {
		t.Fatalf("TLS envelope: %v %v", fetched, err)
	}
	result := make(chan *errx.MailError, 1)
	go func() { result <- c.FetchBody(fetched[0]) }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("TLS literal failure swallowed")
		}
	case <-time.After(time.Second):
		t.Fatal("TLS literal error left decoder blocked")
	}
	connect()
	fetched, err = c.FetchEnvelopes(t.Context(), []goimap.UID{1})
	if err != nil || len(fetched) != 1 {
		t.Fatalf("TLS retry envelope: %v %v", fetched, err)
	}
	if err := c.FetchBody(fetched[0]); err != nil || fetched[0].Email.BodyPlain != "complete body" {
		t.Fatalf("TLS retry body: %q %v", fetched[0].Email.BodyPlain, err)
	}
}
