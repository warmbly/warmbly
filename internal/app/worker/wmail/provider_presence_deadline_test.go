package wmail

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestSyncFolderCapDoesNotAuthorizeRename(t *testing.T) {
	inbox := models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101, HighestModSeq: 100}
	conn := &fakeImapConn{folders: []models.Mailbox{inbox, {Name: "NewlyKeptFolder", UIDValidity: 42, UIDNext: 101, HighestModSeq: 100}}, overflow: 1, present: []string{"StillOnServerButOutsideCap"}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &inbox)
	w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &models.Mailbox{Name: "StillOnServerButOutsideCap", UIDValidity: 42, UIDNext: 101, HighestModSeq: 100})
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, event := range *events {
		if event.eventType == models.JobEventTypeMailboxRename {
			t.Fatalf("bounded listing inferred false rename: %+v", event.body)
		}
	}
}

func TestSyncGraphRetryAfterStopsInflightRequests(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		out.Header().Set("Retry-After", "720")
		out.Header().Set("Content-Type", "application/json")
		out.WriteHeader(http.StatusServiceUnavailable)
		_, _ = out.Write([]byte(`{"error":{"code":"ServiceUnavailable"}}`))
	}))
	defer srv.Close()
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, srv, &events, &relayed)
	previous := seedSyncTruth(w, &relayed)
	err := w.syncOnce(t.Context())
	if err == nil || err.RetryAfter != 12*time.Minute || requests.Load() != 1 || err.Failure == nil || err.Failure.RetryAt == nil {
		t.Fatalf("early hidden retry or lost deadline: requests=%d err=%+v", requests.Load(), err)
	}
	if time.Now().Add(w.nextSyncDelay(time.Minute, err)).Before(*err.Failure.RetryAt) {
		t.Fatal("next pass undercuts provider deadline")
	}
	assertNoSyncSuccess(t, w, previous)
}

func TestSyncGraphLaterFolderRetryAfterOverridesEarlierError(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 2 {
			out.Header().Set("Retry-After", "720")
		}
		out.Header().Set("Content-Type", "application/json")
		out.WriteHeader(http.StatusServiceUnavailable)
		_, _ = out.Write([]byte(`{"error":{"code":"ServiceUnavailable"}}`))
	}))
	defer srv.Close()
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, srv, &events, &relayed)
	previous := seedSyncTruth(w, &relayed)
	err := w.syncOnce(t.Context())
	if err == nil || err.RetryAfter < 12*time.Minute || requests.Load() != 2 || err.Failure == nil || err.Failure.RetryAt == nil {
		t.Fatalf("later provider deadline discarded: requests=%d err=%+v delay=%s", requests.Load(), err, w.nextSyncDelay(time.Minute, err))
	}
	if time.Now().Add(w.nextSyncDelay(time.Minute, err)).Before(*err.Failure.RetryAt) {
		t.Fatal("later deadline was undercut by backoff")
	}
	assertNoSyncSuccess(t, w, previous)
}

func TestSyncFolderCapPreservesUnselectedStateWithoutClaimingRecovery(t *testing.T) {
	inbox := models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101, HighestModSeq: 100}
	conn := &fakeImapConn{folders: []models.Mailbox{inbox}, overflow: 1, present: []string{"StillOnServerButOutsideCap"}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &inbox)
	w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &models.Mailbox{Name: "StillOnServerButOutsideCap", UIDValidity: 42, UIDNext: 101, HighestModSeq: 100})
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	w.transportFailures = 2
	w.EmailType = models.InboxProviderSMTPIMAP
	if err := w.syncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gone := mailboxDeletes(*events); len(gone) != 0 {
		t.Fatalf("bounded listing overflow=%d retired a folder without absence evidence: %v", conn.overflow, gone)
	}
	assertNoSyncSuccess(t, w, previous)
	if w.transportFailures != 2 || len(w.SmtpImapData.Mailboxes) != 2 {
		t.Fatal("capped pass erased saved folder or transport warning")
	}
	conn.overflow = 0
	conn.folders = append(conn.folders, *w.SmtpImapData.Mailboxes[1])
	if err := w.syncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSyncCatchUp(t, w, previous)
	if w.transportFailures != 0 {
		t.Fatal("complete uncapped pass did not confirm recovery")
	}
}

func TestSyncFolderCapRetiresOnlyCompleteAbsenceOrExplicitExclusion(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "excluded"}[excluded], func(t *testing.T) {
			inbox := models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101, HighestModSeq: 100}
			conn := &fakeImapConn{folders: []models.Mailbox{inbox}, overflow: 1, present: []string{"RetainedOutsideCap"}}
			budget := &skipBudget{fixedBudget: &fixedBudget{allow: 10}}
			if excluded {
				budget.skip = []string{"Retired"}
				conn.present = append(conn.present, "Retired")
			}
			w, events := newIMAPTestMail(conn, budget, &inbox)
			w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes,
				&models.Mailbox{Name: "Retired", UIDValidity: 42, UIDNext: 101, HighestModSeq: 100},
				&models.Mailbox{Name: "RetainedOutsideCap", UIDValidity: 43, UIDNext: 101, HighestModSeq: 100})
			if err := w.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			deletes := mailboxEvents(*events, models.JobEventTypeMailboxDelete)
			if len(deletes) != 1 {
				t.Fatalf("expected one positively authorized retirement: %+v", deletes)
			}
			retired := deletes[0].body.(*models.JobEventMailboxDelete)
			if retired.Mailbox != "Retired" || retired.Skipped != excluded || len(w.SmtpImapData.Mailboxes) != 2 {
				t.Fatalf("wrong cap retirement: %+v", retired)
			}
		})
	}
}

func TestNextSyncDelayPreservesRetryAfterAcrossBackoff(t *testing.T) {
	for _, code := range []errx.MailErrorCode{errx.MailErrorCodeSendingTooFast, errx.MailErrorCodeServerUnreachable, errx.MailErrorCodeConnectionLost} {
		for _, deadline := range []time.Duration{30 * time.Second, 90 * time.Second, 12 * time.Minute, time.Hour} {
			for _, failures := range []int{0, 1, 3, 9} {
				w := &WMail{transportFailures: failures}
				mailErr := errx.MError(errx.MailErrorWarning, code, "fixture", errx.MailErrorResolveMethodRetry)
				mailErr.RetryAfter = deadline
				for range 500 {
					if got := w.nextSyncDelay(time.Minute, mailErr); got < deadline {
						t.Fatalf("code=%s failures=%d deadline=%s delay=%s", code, failures, deadline, got)
					}
				}
			}
		}
	}
}

func TestSyncPresentFolderWithoutStatusRetainsRecoveryEvidence(t *testing.T) {
	inbox := models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101, HighestModSeq: 100}
	conn := &fakeImapConn{folders: []models.Mailbox{inbox}, present: []string{"Unselectable"}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &inbox)
	w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &models.Mailbox{Name: "Unselectable", UIDValidity: 42, UIDNext: 101, HighestModSeq: 100})
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	w.EmailType = models.InboxProviderSMTPIMAP
	w.transportFailures = 2
	if err := w.syncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertNoSyncSuccess(t, w, previous)
	if len(mailboxDeletes(*events)) != 0 || w.transportFailures != 2 {
		t.Fatal("presence without usable status retired state or cleared warning")
	}
}
