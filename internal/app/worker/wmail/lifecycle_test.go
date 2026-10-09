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
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

type closingIMAP struct {
	ImapConn
	closes atomic.Int32
}

type observedIMAPSession struct {
	imapserver.Session
	closed chan struct{}
}

func (s *observedIMAPSession) Close() error { defer close(s.closed); return s.Session.Close() }

func TestMailboxLifecycleStopClosesAuthenticatedIdleSocket(t *testing.T) {
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("mailbox@example.test", "local-test")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(user)
	closed := make(chan struct{})
	srv := imapserver.New(&imapserver.Options{Caps: goimap.CapSet{goimap.CapIMAP4rev1: {}}, InsecureAuth: true, NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &observedIMAPSession{Session: mem.NewSession(), closed: closed}, nil, nil
	}})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	d := &models.AddWorkerEmail{ID: uuid.New(), UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP, Email: "mailbox@example.test", ImapSync: true, SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{IMAP: &models.Service{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Username: "mailbox@example.test", Password: "local-test", Security: models.MailSecurityNone}}}}
	w, merr := NewWMail(d, func(models.JobEventType, string, any) error { return nil }, nil, nil, nil, nil, nil, nil)
	if merr != nil {
		t.Fatal(merr)
	}
	w.Stop()
	w.Stop()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("authenticated idle server session retained after stop")
	}
}

func (c *closingIMAP) Close() error { c.closes.Add(1); return nil }

func TestMailboxLifecycleStopDrainsAndRejectsStaleWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &closingIMAP{}
	w := &WMail{Ctx: ctx, Cancel: cancel, SmtpImapData: &SmtpImapData{ImapClient: c}}
	done, ok := w.BeginExecution()
	if !ok {
		t.Fatal("live execution rejected")
	}
	w.Stop()
	w.Stop()
	if c.closes.Load() != 1 || ctx.Err() == nil {
		t.Fatal("stop must cancel and close once")
	}
	if _, ok := w.BeginExecution(); ok {
		t.Fatal("retired execution admitted")
	}
	drained := make(chan struct{})
	go func() { w.Drain(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("drained before admitted work returned")
	default:
	}
	done()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("drain did not finish")
	}
}

func TestMailboxLifecycleExecutionKeyAndTokenRefresh(t *testing.T) {
	token := &oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}
	d := &models.AddWorkerEmail{ID: uuid.New(), UserID: uuid.New(), Type: models.InboxProviderGoogle, Google: &models.AddWorkerEmailGoogleData{Token: token}}
	w := &WMail{onEvent: func(models.JobEventType, any) error { return nil }}
	w.rememberExecution(d)
	changed := *d
	changed.Sync = &models.AddWorkerEmailSyncData{Policy: models.SyncPolicy{DailyMessages: 99}, State: &models.SyncState{BackfillStatus: models.SyncBackfillComplete}}
	if !w.SameExecution(&changed) {
		t.Fatal("policy/checkpoint publication restarted provider")
	}
	g := *d.Google
	changed.Google = &g
	g.Token = &oauth2.Token{AccessToken: "fresh", RefreshToken: "rotated", Expiry: time.Now().Add(time.Hour)}
	if w.SameExecution(&changed) {
		t.Fatal("local reauthorization ignored")
	}
	if err := w.onTokenUpdate(g.Token); err != nil {
		t.Fatal(err)
	}
	if !w.SameExecution(&changed) || !w.SameExecution(d) {
		t.Fatal("routine token refresh/repeated original publication restarted provider")
	}
	for i := 0; i < 10; i++ {
		if err := w.onTokenUpdate(&oauth2.Token{AccessToken: string(rune('a' + i)), RefreshToken: "rotated"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.executionKeys) > 3 {
		t.Fatal("refresh aliases grew without bound")
	}
	brokered := *d
	brokered.Brokered = true
	w.rememberExecution(&brokered)
	changed.Brokered = true
	if !w.SameExecution(&changed) {
		t.Fatal("broker-owned token rotation restarted provider")
	}
	w.Stop()
	w.onEvent = func(models.JobEventType, any) error { t.Fatal("stale token callback published"); return nil }
	if err := w.onTokenUpdate(token); err != nil {
		t.Fatal(err)
	}
}

func TestMailboxLifecycleLegacyDefaultsAndNormalizedSkipSettings(t *testing.T) {
	d := &models.AddWorkerEmail{ID: uuid.New(), UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP}
	w := &WMail{}
	w.rememberExecution(d)
	copyData := *d
	v := true
	copyData.SaveToSent = &v
	copyData.Sync = &models.AddWorkerEmailSyncData{Policy: models.SyncPolicy{DailyMessages: 99}}
	if !w.SameExecution(&copyData) {
		t.Fatal("old backend missing sync/sent fields differs from effective defaults")
	}
	copyData.Sync.Policy.SkipFolders = []string{" Custom ", "CUSTOM", ""}
	w.rememberExecution(&copyData)
	copyData.Sync = &models.AddWorkerEmailSyncData{Policy: models.SyncPolicy{SkipFolders: []string{"custom"}, DailyMessages: 120}}
	if !w.SameExecution(&copyData) {
		t.Fatal("same effective folder skips restarted client")
	}
}

func TestMailboxLifecycleResumePreservesCompletedStateAndProviderCursors(t *testing.T) {
	id, user := uuid.New(), uuid.New()
	stamp := time.Now().Add(-time.Hour)
	state := models.SyncState{BackfillStatus: models.SyncBackfillComplete, BackfillSynced: 12, BackfillCompletedAt: &stamp, LastSyncedAt: &stamp, Deferred: 5, BackfillCursor: models.SyncCursor{PageToken: "page", Folders: map[string]models.SyncFolderCursor{"Inbox": {UID: 8, Done: true}}}}
	for _, provider := range []models.InboxProvider{models.InboxProviderGoogle, models.InboxProviderOutlook, models.InboxProviderSMTPIMAP} {
		t.Run(string(provider), func(t *testing.T) {
			old := &WMail{ID: id, UserID: user, EmailType: provider, tracker: newSyncTracker(&state, func(models.SyncState) error { return nil }), unmapPending: map[string]uuid.UUID{"pending": uuid.New()}}
			d := &models.AddWorkerEmail{ID: id, UserID: user, Type: provider}
			switch provider {
			case models.InboxProviderGoogle:
				old.GoogleData = &GoogleData{LastHistoryID: 91}
				d.Google = &models.AddWorkerEmailGoogleData{LastHistoryID: 1}
			case models.InboxProviderOutlook:
				old.GraphData = &GraphData{Client: &msgraph.Client{DeltaLinks: map[string]string{"Inbox": "live"}}}
				d.Graph = &models.AddWorkerEmailGraphData{DeltaLinks: map[string]string{"Inbox": "stale"}}
			case models.InboxProviderSMTPIMAP:
				old.SmtpImapData = &SmtpImapData{Mailboxes: []*models.Mailbox{{Name: "Inbox", UIDValidity: 4, UIDNext: 17}}}
				d.SmtpImap = &models.AddWorkerEmailSmtpImapData{}
			}
			old.Stop()
			old.Drain()
			old.ResumeInto(d)
			if d.Sync.State.BackfillStatus != models.SyncBackfillComplete || d.Sync.State.BackfillSynced != 12 || d.Sync.State.Deferred != 5 || d.Sync.State.LastSyncedAt != &stamp || d.Sync.State.BackfillCursor.PageToken != "page" {
				t.Fatal("replacement reset completed/live state")
			}
			d.Sync.State.BackfillCursor.Folders["Inbox"] = models.SyncFolderCursor{}
			if old.tracker.state.BackfillCursor.Folders["Inbox"].UID != 8 {
				t.Fatal("replacement aliases retired mutable state")
			}
			if d.Google != nil && d.Google.LastHistoryID != 91 {
				t.Fatal("Gmail cursor lost")
			}
			if d.Graph != nil && d.Graph.DeltaLinks["Inbox"] != "live" {
				t.Fatal("Graph cursor lost")
			}
			if d.SmtpImap != nil && (len(d.SmtpImap.Mailboxes) != 1 || d.SmtpImap.Mailboxes[0].UIDNext != 17) {
				t.Fatal("IMAP cursor lost")
			}
			next := &WMail{UserID: user, EmailType: provider, tracker: newSyncTracker(d.Sync.State, func(models.SyncState) error { return nil })}
			old.tracker.dirty = true
			next.PreservePending(old)
			if next.unmapPending["pending"] != old.unmapPending["pending"] || !next.tracker.dirty {
				t.Fatal("pending retries/state flush lost")
			}
		})
	}
}

func TestMailboxLifecycleAuthTerminationClosesOnceAndIgnoresRetiredErrors(t *testing.T) {
	c := &closingIMAP{}
	events := 0
	w := &WMail{SmtpImapData: &SmtpImapData{ImapClient: c}, onEvent: func(models.JobEventType, any) error { events++; return nil }}
	w.CaptureError(errx.ErrMailServerUnreachable)
	if c.closes.Load() != 0 {
		t.Fatal("transport timeout permanently retired mailbox")
	}
	w.CaptureError(&errx.MailError{Type: errx.MailErrorCritical, Code: errx.MailErrorCodeInvalidCredentials})
	w.CaptureError(&errx.MailError{Type: errx.MailErrorCritical, Code: errx.MailErrorCodeInvalidCredentials})
	if c.closes.Load() != 1 || events != 2 {
		t.Fatalf("closes=%d events=%d", c.closes.Load(), events)
	}
}
