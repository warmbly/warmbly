package wmail

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func catchUpMail(t *testing.T, provider models.InboxProvider, state models.SyncState) *WMail {
	t.Helper()
	w := &WMail{EmailType: provider, gov: newGovernor(uuid.New(), nil, nil, models.SyncPolicy{FullCheckSeconds: 3600})}
	w.tracker = newSyncTracker(&state, func(models.SyncState) error { return nil })
	return w
}

// An import or recovery only the full pass advances keeps the full pass on the catch-up cadence.
func TestFullPassRunsOnTheCatchUpCadenceWhileWorkIsPending(t *testing.T) {
	done := models.SyncState{BackfillStatus: models.SyncBackfillComplete}

	importing := catchUpMail(t, models.InboxProviderSMTPIMAP, models.SyncState{BackfillStatus: models.SyncBackfillRunning})
	importing.SmtpImapData = &SmtpImapData{}
	if got := importing.fullPassInterval(nil); got != config.SyncCatchUpInterval {
		t.Fatalf("running import: %v", got)
	}

	idle := catchUpMail(t, models.InboxProviderSMTPIMAP, done)
	idle.SmtpImapData = &SmtpImapData{}
	if got := idle.fullPassInterval(nil); got != time.Hour {
		t.Fatalf("nothing pending: %v, want the full check interval", got)
	}
	failed := errx.MError(errx.MailErrorWarning, errx.MailErrorCodeServerUnreachable, "down", errx.MailErrorResolveMethodRetry)
	if got := importing.fullPassInterval(failed); got != time.Hour {
		t.Fatalf("a failed pass took the catch-up cadence: %v", got)
	}

	gmail := catchUpMail(t, models.InboxProviderGoogle, done)
	gmail.GoogleData = &GoogleData{LastHistoryID: 5}
	if gmail.fullPassPending() {
		t.Fatal("a caught-up Gmail mailbox reads as pending")
	}
	gmail.tracker.state.BackfillCursor.GoogleRecovery = &models.GoogleHistoryRecovery{HistoryID: 9, Since: time.Now()}
	if !gmail.fullPassPending() {
		t.Fatal("a Gmail history recovery does not read as pending")
	}

	links := map[string]string{}
	for _, f := range msgraph.TrackedFolders {
		if f != msgraph.FolderArchive {
			links[f] = "https://graph.microsoft.com/v1.0/me/mailFolders/" + f + "/messages/delta?$deltatoken=t"
		}
	}
	graph := catchUpMail(t, models.InboxProviderOutlook, done)
	graph.GraphData = &GraphData{Client: &msgraph.Client{DeltaLinks: links}}
	if graph.fullPassPending() {
		t.Fatal("a caught-up Graph mailbox without an archive reads as pending")
	}
	delete(links, msgraph.FolderInbox)
	if !graph.fullPassPending() {
		t.Fatal("an Inbox on its first walk does not read as pending")
	}
}

// A narrow check that meets work only the full pass does asks for the full pass.
func TestNarrowCheckFlagsWorkItLeavesToTheFullPass(t *testing.T) {
	var events []captured
	g := newFakeGraph()
	w := newGraphTestMail(t, g.serve(t), &events, new([]models.SyncState))
	if merr := w.syncGraph(t.Context(), true); merr != nil || w.narrowDeferred {
		t.Fatalf("a caught-up mailbox deferred work: %v %v", merr, w.narrowDeferred)
	}
	delete(w.GraphData.Client.DeltaLinks, msgraph.FolderInbox)
	if merr := w.syncGraph(t.Context(), true); merr != nil || !w.narrowDeferred {
		t.Fatalf("an Inbox on its first walk was not flagged: %v", merr)
	}

	gm := newGoogleTestMail(t, (&gmailWakeServer{}).serve(t), &events)
	if merr := gm.syncGoogle(t.Context(), true); merr != nil || !gm.narrowDeferred {
		t.Fatalf("a Gmail mailbox without a baseline was not flagged: %v", merr)
	}

	conn := &wakeImapConn{fakeImapConn: &fakeImapConn{folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 9, HighestModSeq: 200}}}}
	im, _ := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})
	if merr := im.imapWakeCheck(t.Context()); merr != nil || !im.narrowDeferred {
		t.Fatalf("a re-generated INBOX was not flagged: %v", merr)
	}
}
