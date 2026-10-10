package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestWarmupFilingRetainedProviderFloorSurvivesAdmissionDowngrade(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending bool
		denied  bool
	}{{"legacy-pending", true, false}, {"pending-false", false, false}, {"modern-denial", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			account, worker, filing := uuid.New(), uuid.New(), uuid.New()
			at := time.Now().UTC().Add(20 * time.Minute)
			failure := *errx.ErrMailServerUnreachable
			failure.Failure = &errx.SendFailure{RetryAt: &at}
			imap := &filingIMAP{found: map[string]uint32{"INBOX": 11}, err: &failure}
			w, bus := filingWorker(account, imap)
			w.ID = worker.String()
			admissions, deferrals := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer synthetic-review" {
					t.Error("missing native node auth")
				}
				switch req.URL.Path {
				case "/api/v1/internal/worker/warmup-actions":
					admissions++
					decision := models.WarmupActionDecision{Actions: []string{models.WarmupActionFile}, FilingPending: tc.pending}
					if admissions == 1 {
						decision.FilingPending = true
						decision.FilingRecovery = &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: account, WorkerID: worker, FilingID: filing}
					} else if tc.denied {
						decision.Actions = nil
					}
					_ = json.NewEncoder(out).Encode(decision)
				case "/api/v1/internal/worker/warmup-actions/defer":
					deferrals++
					out.WriteHeader(http.StatusServiceUnavailable)
				default:
					t.Errorf("unexpected authority path %s", req.URL.Path)
				}
			}))
			defer srv.Close()
			authority, err := repository.NewHTTPSyncContextRepository(srv.URL, "synthetic-review")
			if err != nil {
				t.Fatal(err)
			}
			w.SyncContextRepository = authority
			action := models.WarmupEmailAction{EmailID: account, FilingID: filing.String(), RFCMessageID: "<downgrade@example.test>", Actions: []string{models.WarmupActionFile}}
			if err := w.HandleWarmupAction(t.Context(), action); err == nil || imap.moves != 1 || deferrals != 1 || len(bus.events) != 0 {
				t.Fatalf("precondition: failed modern persistence not retained: err=%v provider=%d deferrals=%d events=%v", err, imap.moves, deferrals, bus.events)
			}
			retained, ok := w.pendingFilingDeferrals.Load(filing)
			if !ok || !retained.(models.WarmupFilingDeferralRequest).ProviderRetryAt.Equal(at) {
				t.Fatal("precondition: exact native floor not retained", retained)
			}
			if tc.denied {
				delete(w.mailManager.Emails, account)
				if err := w.HandleWarmupAction(t.Context(), action); err != nil || imap.moves != 1 || deferrals != 1 || len(bus.events) != 0 {
					t.Fatalf("authoritative denial touched mailbox/persistence or acknowledged filing: err=%v provider=%d deferrals=%d events=%v", err, imap.moves, deferrals, bus.events)
				}
				if _, found := w.pendingFilingDeferrals.Load(filing); found {
					t.Fatal("authoritative denial retained canceled filing floor")
				}
				return
			}
			err = w.HandleWarmupAction(t.Context(), action)
			if err == nil || imap.moves != 1 || deferrals != 2 || len(bus.events) != 0 {
				t.Fatalf("downgraded admission bypassed retained native floor: pending=%v err=%v provider=%d deferrals=%d events=%v", tc.pending, err, imap.moves, deferrals, bus.events)
			}
		})
	}
}

func TestWarmupFilingLegacyHTTPAuthorityKeepsFiveAttemptPolicy(t *testing.T) {
	account, worker := uuid.New(), uuid.New()
	at := time.Now().UTC().Add(20 * time.Minute)
	failure := *errx.ErrMailServerUnreachable
	failure.Failure = &errx.SendFailure{RetryAt: &at}
	imap := &filingIMAP{found: map[string]uint32{"INBOX": 11}, err: &failure}
	w, bus := filingWorker(account, imap)
	w.ID = worker.String()
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/internal/worker/warmup-actions" || req.Header.Get("Authorization") != "Bearer synthetic-review" {
			t.Error("legacy request bypassed admission or attempted unsupported deferral")
			out.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(out).Encode(models.WarmupActionDecision{Actions: []string{models.WarmupActionFile}})
	}))
	defer srv.Close()
	authority, err := repository.NewHTTPSyncContextRepository(srv.URL, "synthetic-review")
	if err != nil {
		t.Fatal(err)
	}
	w.SyncContextRepository = authority
	action := models.WarmupEmailAction{EmailID: account, FilingID: uuid.NewString(), RFCMessageID: "<legacy-five@example.test>", Actions: []string{models.WarmupActionFile}}
	for attempt := 1; attempt <= warmupDeleteRedeliveries; attempt++ {
		ctx := context.WithValue(t.Context(), deliveryKey{}, delivery{attempt: attempt, redelivers: true})
		err := w.HandleWarmupAction(ctx, action)
		if (err != nil) != (attempt < warmupDeleteRedeliveries) || imap.moves != attempt || len(bus.events) != 0 {
			t.Fatalf("legacy five-attempt behavior changed: attempt=%d provider=%d err=%v events=%v", attempt, imap.moves, err, bus.events)
		}
		if _, found := w.pendingFilingDeferrals.Load(uuid.MustParse(action.FilingID)); found {
			t.Fatal("first-use legacy response retained unsupported modern floor")
		}
	}
}
