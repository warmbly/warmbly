package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/models"
)

type closedWorkerIMAP struct {
	wmail.ImapConn
	closes atomic.Int32
}

func (c *closedWorkerIMAP) Close() error { c.closes.Add(1); return nil }

func TestAddEmailReplacesChangedCredentialsAndRemoveClosesInstalledSession(t *testing.T) {
	w := newLoadingWorker(&capturedEvents{})
	id := uuid.New()
	d := &models.AddWorkerEmail{ID: id, UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP, Email: "a@example.test", SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{SMTP: &models.Service{Host: "smtp.example.test", Port: 587, Password: "old"}, IMAP: &models.Service{Host: "imap.example.test", Port: 993, Password: "old"}}}, Sync: &models.AddWorkerEmailSyncData{State: &models.SyncState{BackfillStatus: models.SyncBackfillComplete, BackfillSynced: 12}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.HandleAddEmail(ctx, d); err != nil {
		t.Fatal(err)
	}
	old, ok := w.loadedMailbox(ctx, id)
	if !ok {
		t.Fatal("initial load failed")
	}
	conn := &closedWorkerIMAP{}
	old.SmtpImapData.ImapClient = conn
	for i := 0; i < 3; i++ {
		if err := w.HandleAddEmail(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	copyData := *d
	copyData.Sync = &models.AddWorkerEmailSyncData{Policy: models.SyncPolicy{DailyMessages: 88}}
	if err := w.HandleAddEmail(ctx, &copyData); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.loadedMailbox(ctx, id); got != old || conn.closes.Load() != 0 {
		t.Fatal("policy/duplicate ADD_EMAIL retired healthy client")
	}
	s := *d.SmtpImap
	creds := *s.Credentials
	smtp := *creds.SMTP
	smtp.Password = "new"
	creds.SMTP = &smtp
	s.Credentials = &creds
	copyData.SmtpImap = &s
	if err := w.HandleAddEmail(ctx, &copyData); err != nil {
		t.Fatal(err)
	}
	current, ok := w.loadedMailbox(ctx, id)
	if !ok || current == old || current.SmtpImapData.SmtpClient.Credentials.Password != "new" || conn.closes.Load() != 1 {
		t.Fatal("still-loaded credentials were not replaced and retired")
	}
	currentConn := &closedWorkerIMAP{}
	current.SmtpImapData.ImapClient = currentConn
	old.Terminate()
	if w.mailManager.Get(id) != current || currentConn.closes.Load() != 0 {
		t.Fatal("stale old termination closed replacement")
	}
	rem := &models.RemoveWorkerEmail{EmailID: id.String()}
	for i := 0; i < 2; i++ {
		if err := w.HandleRemoveEmail(ctx, rem); err != nil {
			t.Fatal(err)
		}
	}
	if w.mailManager.Get(id) != nil || currentConn.closes.Load() != 1 {
		t.Fatal("installed removal did not close exactly once")
	}
	if err := w.HandleAddEmail(ctx, &copyData); err != nil {
		t.Fatal(err)
	}
	if mail, ok := w.loadedMailbox(ctx, id); !ok || mail == current {
		t.Fatal("mailbox could not reload after removal")
	}
	if err := w.HandleRemoveEmail(ctx, rem); err != nil {
		t.Fatal(err)
	}
}
