package wmail

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestControlPlane503EvidenceHoldsCursorsAndIsNotProviderFailure(t *testing.T) {
	var got nodeevidence.Event
	nodeevidence.Install(func(e nodeevidence.Event) { got = e })
	t.Cleanup(func() { nodeevidence.Install(nil) })
	w := &WMail{ID: uuid.New()}
	stats := &tickStats{}
	if err := w.controlPlaneError(fmt.Errorf("wrapped: %w", &repository.ControlPlaneHTTPError{Status: 503}), stats); err != nil {
		t.Fatal("control-plane failure became a provider error", err)
	}
	if !stats.aborted || got.Name != nodeevidence.ControlPlaneHeld || got.Category != "control_plane" || got.HTTPStatus != 503 || got.MailboxID != w.ID {
		t.Fatal("missing cursor-held evidence", got)
	}
}

// A failed MAILBOX_DELETE publish used to end the IMAP pass with no log and no
// evidence, so the mailbox only showed up as "No recent successful sync check"
// (#1157). It still ends the pass and holds state, but now says why.
func TestImapSyncPublishFailureIsReported(t *testing.T) {
	var got nodeevidence.Event
	nodeevidence.Install(func(e nodeevidence.Event) { got = e })
	t.Cleanup(func() { nodeevidence.Install(nil) })
	conn := &fakeImapConn{folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100}}}
	w, _ := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})
	w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &models.Mailbox{Name: "Old", UIDValidity: 5, HighestModSeq: 10})
	w.onEvent = func(models.JobEventType, any) error { return fmt.Errorf("bus down") }

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got.Name != nodeevidence.ControlPlaneHeld || got.MailboxID != w.ID {
		t.Fatal("publish failure left no evidence", got)
	}
	if len(w.SmtpImapData.Mailboxes) != 2 {
		t.Fatal("the folder was retired although its delete was never published")
	}
}
