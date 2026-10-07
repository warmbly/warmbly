package wmail

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/diagnosticauth"
	"github.com/warmbly/warmbly/internal/repository"
)

type diagnosticAuthorityStub struct {
	repository.SyncContextRepository
	allow    bool
	requests []models.DiagnosticAuthRequest
}

func (a *diagnosticAuthorityStub) DiagnosticAuth(_ context.Context, req models.DiagnosticAuthRequest) (*models.DiagnosticAuthGrant, error) {
	a.requests = append(a.requests, req)
	if !a.allow {
		return nil, errors.New("send result not stamped")
	}
	if req.Result != nil {
		return nil, nil
	}
	return &models.DiagnosticAuthGrant{Nonce: uuid.New(), MessageID: "probe@example.test", From: "sender@example.test", To: "recipient@example.test"}, nil
}

type diagnosticIMAPStub struct {
	ImapConn
	calls int
	raw   []byte
}

func (c *diagnosticIMAPStub) DiagnosticRawMessage(ctx context.Context, folder string, validity, uid uint32, limit int) ([]byte, error) {
	if folder != "Diagnostic" || validity != 17 || uid != 4 || limit != diagnosticauth.MaxMIMEBytes || ctx.Err() != nil {
		return nil, errors.New("incorrect raw locator/bound")
	}
	c.calls++
	c.raw = []byte("From: sender@example.test\r\nTo: recipient@example.test\r\nMessage-ID: <probe@example.test>\r\n\r\nDiagnostic fixture\r\n")
	return c.raw, nil
}

func TestDiagnosticRetryWaitsForSendResultAndDefersIMAPUntilSyncReleasesMutex(t *testing.T) {
	a, c := &diagnosticAuthorityStub{}, &diagnosticIMAPStub{}
	w := &WMail{ID: uuid.New(), ExecutorID: uuid.New(), EmailType: models.InboxProviderSMTPIMAP, SyncContext: a, SmtpImapData: &SmtpImapData{ImapClient: c}}
	token := uuid.New()
	w.verifyDiagnostic(t.Context(), &models.EmailMessageData{MessageID: "probe@example.test", UID: 4, Flags: []string{config.WarmupVerifyHeader + ":" + token.String()}}, &models.EmailMessageStoreData{FolderPath: "Diagnostic", Mailbox: 17})
	if c.calls != 0 || len(a.requests) != 0 || len(w.diagnosticRetries) != 1 {
		t.Fatal("retrieved raw while sync still owns mutex")
	}
	w.retryDiagnostics(t.Context())
	if c.calls != 0 || len(a.requests) != 1 {
		t.Fatal("denied authority retrieved MIME")
	}
	a.allow = true
	for attempt := 1; attempt < 3; attempt++ {
		retry := w.diagnosticRetries[token]
		retry.next = time.Time{}
		w.diagnosticRetries[token] = retry
		w.retryDiagnostics(t.Context())
	}
	if c.calls != 2 {
		t.Fatal("later exact result did not enable bounded retry", c.calls)
	}
	for _, b := range c.raw {
		if b != 0 {
			t.Fatal("worker retained raw MIME")
		}
	}
	w.retryDiagnostics(t.Context())
	if len(w.diagnosticRetries) != 0 || c.calls != 2 {
		t.Fatal("unknown retry was unbounded")
	}
	for _, req := range a.requests {
		if req.Token != token || req.MailboxID != w.ID || req.WorkerID != w.ExecutorID || req.MessageID != "probe@example.test" {
			t.Fatal("retry widened authorization context", req)
		}
	}
}

func TestDiagnosticRetryExpiresAndIgnoresOrdinaryMessages(t *testing.T) {
	a := &diagnosticAuthorityStub{allow: true}
	w := &WMail{ExecutorID: uuid.New(), SyncContext: a}
	w.verifyDiagnostic(t.Context(), &models.EmailMessageData{MessageID: "ordinary@example.test"}, &models.EmailMessageStoreData{})
	if len(a.requests) != 0 || len(w.diagnosticRetries) != 0 {
		t.Fatal("ordinary inbox message retrieved")
	}
	token := uuid.New()
	w.diagnosticRetries = map[uuid.UUID]diagnosticRetry{token: {expires: time.Now().Add(-time.Minute)}}
	w.retryDiagnostics(t.Context())
	if len(a.requests) != 0 || len(w.diagnosticRetries) != 0 {
		t.Fatal("expired retry executed")
	}
}
