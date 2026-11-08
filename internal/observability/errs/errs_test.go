package errs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// A deployment that configured no backend is the self-host default, so that
// path has to keep working on its own: the event reaches the process log and
// nothing leaves the machine.
func TestNoBackendReportsToTheLog(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if err := Init(Config{Service: "backend", Environment: "dev"}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	CaptureException(errors.New("boom"))
	CaptureMessage("starting")
	Recover("panicked")

	// A caller that reports unconditionally must not mint an issue with
	// nothing in it.
	CaptureException(nil)
	CaptureMessage("")
	Recover(nil)

	got := buf.String()
	if strings.Count(got, "[issue-local]") != 3 {
		t.Errorf("an empty report was logged: %s", got)
	}
	for _, want := range []string{"[issue-local][backend][error] boom", "[issue-local][backend][info] starting", "[issue-local][backend][panic] panicked"} {
		if !strings.Contains(got, want) {
			t.Errorf("log is missing %q, got %s", want, got)
		}
	}
}

// The one thing worth asserting about the PostHog backend is the shape of what
// it posts: PostHog reads an exception out of $exception_list and nothing else,
// so an event that carries the error anywhere else is an event that never
// becomes an issue.
func TestPostHogPostsAnException(t *testing.T) {
	bodies := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	if err := Init(Config{
		PostHogKey:  "phc_test",
		PostHogHost: srv.URL,
		Service:     "worker",
		Environment: "prod",
		Release:     "v1.2.3",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	CaptureException(errors.New("boom"), Tag("mailbox_id", "m-1"))
	// Flushing closes the client, which is why this is the last thing the test
	// asks of it.
	Flush(5 * time.Second)

	var body string
	select {
	case body = <-bodies:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the capture host")
	}

	for _, want := range []string{
		`"$exception"`,
		`"$exception_list"`,
		`"boom"`,
		// The tag the caller attached, flattened into the event properties.
		`"mailbox_id"`,
		// The process, not a person, and no profile built for it.
		`"warmbly-worker"`,
		`"$process_person_profile":false`,
		// The frame the capture came from, so the issue groups by call site.
		`errs.TestPostHogPostsAnException`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("captured body is missing %s: %s", want, body)
		}
	}
}

// A rolling restart cancels every request in flight, and each one unwound
// through a repository that reported the failed query. Nine issues naming
// whichever statements happened to be running is not a deploy anybody needs
// told about, so a cancelled context must not reach a backend at all. A
// deadline this process set still must.
func TestCancelledWorkIsNotReported(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if err := Init(Config{Service: "backend", Environment: "dev"}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	CaptureException(context.Canceled)
	// Wrapped the way a repository reports it, through the query text.
	CaptureException(fmt.Errorf("queryrow failed: %w", context.Canceled))
	CaptureExceptionContext(ctx, fmt.Errorf("get organization: %w", context.Canceled))

	if strings.Contains(buf.String(), "[issue-local]") {
		t.Errorf("a cancelled request was reported: %s", buf.String())
	}

	CaptureException(fmt.Errorf("queryrow failed: %w", context.DeadlineExceeded))
	if !strings.Contains(buf.String(), "[issue-local]") {
		t.Error("a deadline this process set and blew through was dropped as if the caller had gone away")
	}
}

// A failed database connection reaches a remote backend as its SQLSTATE alone:
// pgconn's message names the database user and the addresses it tried.
func TestDatabaseConnectErrorLeavesOutUserAndAddresses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		backend := pgproto3.NewBackend(conn, conn)
		if _, err := backend.ReceiveStartupMessage(); err != nil {
			return
		}
		backend.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "08006", Message: "failed to connect to upstream 10.9.8.7:6432"})
		_ = backend.Flush()
	}()

	_, connectErr := pgconn.Connect(context.Background(),
		"postgres://pscale_api_secretname@"+ln.Addr().String()+"/postgres?sslmode=disable&connect_timeout=5")
	if connectErr == nil {
		t.Fatal("connected to a server that refused")
	}

	got := redact(fmt.Errorf("get membership: %w", connectErr)).Error()
	if got != "failed to connect to the database (SQLSTATE 08006)" {
		t.Errorf("reported %q", got)
	}
	for _, leaked := range []string{"pscale_api_secretname", "10.9.8.7", "127.0.0.1"} {
		if strings.Contains(got, leaked) {
			t.Errorf("reported message carries %q: %s", leaked, got)
		}
	}
	if other := errors.New("boom"); redact(other) != other {
		t.Error("an unrelated error was rewritten")
	}
}

// An error marked Sensitive shows only its public sentence, locally and remotely.
func TestSensitiveErrorReachesRemoteAsItsPublicSentence(t *testing.T) {
	raw := errors.New("dial tcp 10.9.8.7:5432: connect: connection refused")
	err := fmt.Errorf("failed to create migrate instance: %w", Sensitive(raw, "failed to open the migration connection"))
	if got := redact(err).Error(); got != "failed to open the migration connection" {
		t.Errorf("reported %q", got)
	}
	if !errors.Is(err, raw) || strings.Contains(err.Error(), "10.9.8.7") {
		t.Errorf("want the original reachable through Unwrap and hidden from the text: %v", err)
	}
}

// A failed request groups by endpoint: its query's ids never reach the report, and the issue keeps the caller's type.
func TestFailedRequestReportsWithoutItsQuery(t *testing.T) {
	cause := &url.Error{Op: "Get", URL: "https://api.example.test/api/v1/internal/email-message-map?email_id=abc&message_id=def", Err: errors.New("connection refused")}
	err := fmt.Errorf("email_message_map.http: get: %w", cause)

	got := redact(err)
	want := `email_message_map.http: get: Get "https://api.example.test/api/v1/internal/email-message-map": connection refused`
	if got.Error() != want {
		t.Errorf("reported %q, want %q", got.Error(), want)
	}
	if typ := exceptionType(got); typ != "*fmt.wrapError" {
		t.Errorf("type = %q, want the original *fmt.wrapError", typ)
	}

	plain := fmt.Errorf("get: %w", &url.Error{Op: "Get", URL: "https://api.example.test/health", Err: errors.New("timeout")})
	if redact(plain) != plain {
		t.Error("an error whose URL has no query should pass through untouched")
	}
}

// Every request in a joined error loses its query, including one that will not parse or carries a quote.
func TestEveryFailedRequestInAnErrorTreeLosesItsQuery(t *testing.T) {
	err := fmt.Errorf("sync: %w", errors.Join(
		&url.Error{Op: "Get", URL: "https://a.example.test/one?token=first", Err: errors.New("refused")},
		&url.Error{Op: "Post", URL: "https://b.example.test/%zz?token=second", Err: errors.New("bad escape")},
		&url.Error{Op: "Get", URL: `https://c.example.test/three?q="third"`, Err: errors.New("timeout")},
	))

	got := redact(err).Error()
	for _, leaked := range []string{"first", "second", "third", "token"} {
		if strings.Contains(got, leaked) {
			t.Errorf("reported %q, which still carries %q", got, leaked)
		}
	}
	for _, kept := range []string{"https://a.example.test/one", "https://b.example.test/%zz", "https://c.example.test/three"} {
		if !strings.Contains(got, kept) {
			t.Errorf("reported %q, want it to keep %q", got, kept)
		}
	}
}

// A URL that prefixes another in the same error leaves nothing of the longer one's query.
func TestOverlappingRequestURLsLoseTheirWholeQuery(t *testing.T) {
	err := errors.Join(
		&url.Error{Op: "Get", URL: "https://a.example.test/x?token=a", Err: errors.New("refused")},
		&url.Error{Op: "Get", URL: "https://a.example.test/x?token=abcdef", Err: errors.New("refused")},
	)
	if got := redact(err).Error(); strings.Contains(got, "bcdef") || strings.Contains(got, "token") {
		t.Errorf("reported %q, which still carries part of a query", got)
	}
}

// Credentials in a URL never reach a report, with or without a query, and a cause's longer URL is cut whole.
func TestReportedURLsLoseCredentialsAndWholeQueries(t *testing.T) {
	err := fmt.Errorf("publish: %w", &url.Error{
		Op:  "Post",
		URL: "https://user:hun#ter?2@broker.example.test/%zz",
		Err: errors.New("redirected to https://api.example.test/x?token=abcdef"),
	})
	err = fmt.Errorf("get: %w", errors.Join(err, &url.Error{Op: "Get", URL: "https://api.example.test/x?token=a", Err: errors.New("refused")}))

	got := redact(err).Error()
	for _, leaked := range []string{"hun", "ter", "user:", "token", "bcdef"} {
		if strings.Contains(got, leaked) {
			t.Errorf("reported %q, which still carries %q", got, leaked)
		}
	}
	if !strings.Contains(got, "https://broker.example.test/%zz") || !strings.Contains(got, "https://api.example.test/x") {
		t.Errorf("reported %q, want each URL kept without its credentials and query", got)
	}
}
