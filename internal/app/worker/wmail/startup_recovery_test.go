package wmail

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type startupIMAPSession struct {
	imapserver.Session
	failure   *goimap.Error
	recovered *atomic.Bool
	logins    *atomic.Int32
}

func (s *startupIMAPSession) Login(username, password string) error {
	s.logins.Add(1)
	if !s.recovered.Load() {
		return s.failure
	}
	return s.Session.Login(username, password)
}

func startupIMAP(t *testing.T, failure *goimap.Error) (*models.AddWorkerEmail, *atomic.Bool, *atomic.Int32) {
	t.Helper()
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("mailbox@example.test", "local-test")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(user)
	recovered, logins := &atomic.Bool{}, &atomic.Int32{}
	srv := imapserver.New(&imapserver.Options{Caps: goimap.CapSet{goimap.CapIMAP4rev1: {}}, InsecureAuth: true, NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &startupIMAPSession{Session: mem.NewSession(), failure: failure, recovered: recovered, logins: logins}, nil, nil
	}})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	previous := time.Now().Add(-time.Hour)
	data := &models.AddWorkerEmail{
		ID: uuid.New(), UserID: uuid.New(), TransportGeneration: uuid.New(), Type: models.InboxProviderSMTPIMAP, Email: "mailbox@example.test", ImapSync: true,
		SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{
			IMAP: &models.Service{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Username: "mailbox@example.test", Password: "local-test", Security: models.MailSecurityNone},
			SMTP: &models.Service{Host: "smtp.example.test", Port: 587, Username: "mailbox@example.test", Password: "local-test"},
		}},
		Sync: &models.AddWorkerEmailSyncData{State: &models.SyncState{LastSyncedAt: &previous, BackfillStatus: models.SyncBackfillComplete}},
	}
	return data, recovered, logins
}

func TestStartupIMAPFailureKeepsSMTPExecutionAndRecoversSync(t *testing.T) {
	data, recovered, _ := startupIMAP(t, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeUnavailable, Text: "Try again later"})
	mail, err := NewWMail(data, func(models.JobEventType, string, any) error { return nil }, nil, nil, nil, nil, nil, nil)
	if err != nil || mail == nil {
		t.Fatalf("temporary IMAP failure discarded the SMTP mailbox: %v", err)
	}
	t.Cleanup(mail.Discard)
	if mail.SmtpImapData.SmtpClient == nil || mail.SmtpImapData.SmtpClient.Credentials != data.SmtpImap.Credentials.SMTP || !mail.SameExecution(data) {
		t.Fatal("mailbox lost SMTP credentials or idempotent reload identity")
	}
	done, ok := mail.BeginExecution()
	if !ok {
		t.Fatal("temporary sync failure blocked send execution")
	}
	done()
	previous := *data.Sync.State.LastSyncedAt
	if mail.syncStartupError == nil || !isTransportError(mail.syncStartupError) || !mail.tracker.state.LastSyncedAt.Equal(previous) {
		t.Fatal("startup failure was not retained or falsely marked sync as recovered")
	}
	recovered.Store(true)
	if err := mail.syncOnce(t.Context(), false); err != nil {
		t.Fatalf("IMAP could not reconnect without reloading the mailbox: %v", err)
	}
	if mail.tracker.state.LastSyncedAt == nil || !mail.tracker.state.LastSyncedAt.After(previous) {
		t.Fatal("provider-confirmed sync did not advance recovery evidence")
	}
}

func TestStartupIMAPFailureEntersBackoffBeforeRedial(t *testing.T) {
	data, _, logins := startupIMAP(t, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeUnavailable})
	observed := make(chan models.EmailErrorEvent, 1)
	mail, err := NewWMail(data, func(kind models.JobEventType, _ string, body any) error {
		if kind == models.JobEventTypeEmailServerError {
			observed <- body.(models.EmailErrorEvent)
		}
		return nil
	}, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mail.Discard)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { mail.StartSyncWorker(ctx); close(done) }()
	select {
	case event := <-observed:
		if event.Operation != models.MailTransportSync || event.TransportGeneration != data.TransportGeneration {
			t.Fatalf("startup warning lost its transport provenance: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startup failure was not relayed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("backoff did not stop on cancellation")
	}
	if logins.Load() != 1 || mail.transportFailures != 1 {
		t.Fatalf("startup failure was immediately retried: logins=%d failures=%d", logins.Load(), mail.transportFailures)
	}
}

func TestStartupIMAPLockoutStaysLoadedButCredentialsStillFail(t *testing.T) {
	for _, tc := range []struct {
		text string
		want errx.MailErrorCode
	}{
		{"LOGIN DENIED -- TOO MANY FAILURES", errx.MailErrorCodeSendingTooFast},
		{"Invalid credentials", errx.MailErrorCodeInvalidCredentials},
	} {
		t.Run(tc.text, func(t *testing.T) {
			data, _, logins := startupIMAP(t, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAuthenticationFailed, Text: tc.text})
			mail, err := NewWMail(data, func(models.JobEventType, string, any) error { return nil }, nil, nil, nil, nil, nil, nil)
			if tc.want == errx.MailErrorCodeInvalidCredentials {
				if mail != nil || err == nil || err.Code != tc.want {
					t.Fatalf("real credential rejection no longer prevents loading: mail=%v err=%v", mail != nil, err)
				}
				return
			}
			if err != nil || mail == nil || mail.syncStartupError == nil || mail.syncStartupError.Code != tc.want {
				t.Fatalf("provider lockout discarded the mailbox: %v", err)
			}
			t.Cleanup(mail.Discard)
			if retry := mail.syncOnce(t.Context(), false); retry == nil || retry.Code != tc.want || retry.RetryAfter <= 0 {
				t.Fatalf("lockout did not survive reconnect: %+v", retry)
			}
			if logins.Load() != 1 {
				t.Fatal("startup lockout allowed another login")
			}
		})
	}
}
