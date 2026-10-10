package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestWarmupFilingHTTPAuthorityCompatibility(t *testing.T) {
	request := models.WarmupActionRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), Actions: []string{models.WarmupActionFile}, FilingID: uuid.NewString()}
	for _, tc := range []struct {
		name, body       string
		status           int
		pending, failure bool
	}{
		{"confirmed", `{"actions":["move_to_warmbly"],"filing_pending":true}`, 200, true, false},
		{"older backend", `{"actions":["move_to_warmbly"]}`, 200, false, false},
		{"not pending", `{"actions":["move_to_warmbly"],"filing_pending":false}`, 200, false, false},
		{"authority error", `{"actions":["move_to_warmbly"],"filing_pending":true}`, 503, false, true},
		{"invalid response", `{"filing_pending":true,"actions":`, 200, false, true},
		{"missing actions", `{"filing_pending":true}`, 200, false, true},
		{"trailing garbage", `{"actions":["move_to_warmbly"],"filing_pending":true} invalid`, 200, false, true},
		{"multiple responses", `{"actions":["move_to_warmbly"],"filing_pending":true}{}`, 200, false, true},
		{"null response", `null`, 200, false, true},
		{"denied actions", `{"actions":null}`, 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got models.WarmupActionRequest
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if got.MailboxID != request.MailboxID || got.WorkerID != request.WorkerID || got.FilingID != request.FilingID || r.URL.Path != "/api/v1/internal/worker/warmup-actions" || r.Header.Get("Authorization") != "Bearer test" {
					t.Error("authority request lost binding or authentication")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			r, err := NewHTTPSyncContextRepository(srv.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.(WarmupActionRecoveryAdmission).AdmitWarmupAction(t.Context(), request)
			if (err != nil) != tc.failure || !tc.failure && out.FilingPending != tc.pending {
				t.Fatalf("pending=%v err=%v", out.FilingPending, err)
			}
		})
	}
}

func TestWarmupFilingDeferralHTTPRequiresCommittedScopedProof(t *testing.T) {
	request := models.WarmupFilingDeferralRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), FilingID: uuid.New(), ProviderRetryAt: time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)}
	proof := &models.WarmupFilingRecoveryProof{Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: request.FilingID, ProviderRetryAt: &request.ProviderRetryAt}
	good := models.WarmupFilingDeferralDecision{FilingRecovery: proof, Persisted: true}
	for _, tc := range []struct {
		name   string
		status int
		out    models.WarmupFilingDeferralDecision
		body   string
		fail   bool
	}{
		{"committed proof", 200, good, "", false},
		{"older backend", 404, good, "", true},
		{"unavailable backend", 503, good, "", true},
		{"uncommitted proof", 200, models.WarmupFilingDeferralDecision{FilingRecovery: proof}, "", true},
		{"missing proof", 200, models.WarmupFilingDeferralDecision{Persisted: true}, "", true},
		{"malformed response", 200, good, "{", true},
		{"trailing response", 200, good, `{} {}`, true},
		{"null response", 200, good, "null", true},
		{"unsupported version", 200, models.WarmupFilingDeferralDecision{FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 2, MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: request.FilingID, ProviderRetryAt: &request.ProviderRetryAt}, Persisted: true}, "", true},
		{"wrong mailbox", 200, models.WarmupFilingDeferralDecision{FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: uuid.New(), WorkerID: request.WorkerID, FilingID: request.FilingID, ProviderRetryAt: &request.ProviderRetryAt}, Persisted: true}, "", true},
		{"wrong worker", 200, models.WarmupFilingDeferralDecision{FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: request.MailboxID, WorkerID: uuid.New(), FilingID: request.FilingID, ProviderRetryAt: &request.ProviderRetryAt}, Persisted: true}, "", true},
		{"wrong filing", 200, models.WarmupFilingDeferralDecision{FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: uuid.New(), ProviderRetryAt: &request.ProviderRetryAt}, Persisted: true}, "", true},
		{"missing deadline", 200, models.WarmupFilingDeferralDecision{FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: request.FilingID}, Persisted: true}, "", true},
		{"shortened deadline", 200, models.WarmupFilingDeferralDecision{FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: request.FilingID, ProviderRetryAt: timePtr(request.ProviderRetryAt.Add(-time.Second))}, Persisted: true}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestCount := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				var got models.WarmupFilingDeferralRequest
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/internal/worker/warmup-actions/defer" || r.Header.Get("Authorization") != "Bearer test" || json.NewDecoder(r.Body).Decode(&got) != nil || got.MailboxID != request.MailboxID || got.WorkerID != request.WorkerID || got.FilingID != request.FilingID || !got.ProviderRetryAt.Equal(request.ProviderRetryAt) {
					t.Error("deferral request lost exact scope or authentication")
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				} else {
					_ = json.NewEncoder(w).Encode(tc.out)
				}
			}))
			defer srv.Close()
			r, err := NewHTTPSyncContextRepository(srv.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.(WarmupFilingDeferralAuthority).DeferWarmupFiling(t.Context(), request)
			if (err != nil) != tc.fail || requestCount != 1 || tc.fail && (out.Persisted || out.FilingRecovery != nil) {
				t.Fatalf("invalid deferral reply treated as durable: %+v err=%v", out, err)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func TestWarmupFilingHTTPAuthorityRejectsUnscopedVersion(t *testing.T) {
	req := models.WarmupActionRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), FilingID: uuid.NewString(), Actions: []string{models.WarmupActionFile}}
	filing := uuid.MustParse(req.FilingID)
	for _, tc := range []struct {
		name, body string
		fail       bool
	}{
		{"exact proof", "", false},
		{"wrong version", `{"actions":["move_to_warmbly"],"filing_pending":true,"filing_recovery":{"protocol":2}}`, true},
		{"missing scope", `{"actions":["move_to_warmbly"],"filing_pending":true,"filing_recovery":{"protocol":1}}`, true},
		{"false pending", `{"actions":["move_to_warmbly"],"filing_pending":false,"filing_recovery":{"protocol":1}}`, true},
		{"wrong actions", `{"actions":["delete"],"filing_pending":true,"filing_recovery":{"protocol":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
					return
				}
				_ = json.NewEncoder(w).Encode(models.WarmupActionDecision{Actions: req.Actions, FilingPending: true, FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: 1, MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: filing}})
			}))
			defer srv.Close()
			r, err := NewHTTPSyncContextRepository(srv.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.(WarmupActionRecoveryAdmission).AdmitWarmupAction(t.Context(), req)
			if (err != nil) != tc.fail || tc.fail && (out.FilingPending || out.FilingRecovery != nil) || !tc.fail && !out.FilingRecovery.ValidFor(req.MailboxID, req.WorkerID, filing) {
				t.Fatalf("unscoped/unsupported admission: %+v %v", out, err)
			}
		})
	}
}

func TestWarmupFilingRemoteDecodersRejectTrailingValidProof(t *testing.T) {
	request := models.WarmupActionRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), FilingID: uuid.NewString(), Actions: []string{models.WarmupActionFile}}
	at := time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)
	filing := uuid.MustParse(request.FilingID)
	proof := &models.WarmupFilingRecoveryProof{Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: filing, ProviderRetryAt: &at}
	admission, err := json.Marshal(models.WarmupActionDecision{Actions: request.Actions, FilingPending: true, FilingRecovery: proof})
	if err != nil {
		t.Fatal(err)
	}
	deferral, err := json.Marshal(models.WarmupFilingDeferralDecision{Persisted: true, FilingRecovery: proof})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", " \n\t", " {}", " null", " []", " true", " {", " junk"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/internal/worker/warmup-actions/defer" {
					_, _ = w.Write(append(append([]byte{}, deferral...), suffix...))
				} else {
					_, _ = w.Write(append(append([]byte{}, admission...), suffix...))
				}
			}))
			defer srv.Close()
			r, err := NewHTTPSyncContextRepository(srv.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			valid := suffix == "" || suffix == " \n\t"
			out, err := r.(WarmupActionRecoveryAdmission).AdmitWarmupAction(t.Context(), request)
			if (err == nil) != valid || !valid && (out.FilingPending || out.FilingRecovery != nil) {
				t.Fatalf("admission accepted trailing document after valid proof: %+v %v", out, err)
			}
			deferred, err := r.(WarmupFilingDeferralAuthority).DeferWarmupFiling(t.Context(), models.WarmupFilingDeferralRequest{MailboxID: request.MailboxID, WorkerID: request.WorkerID, FilingID: filing, ProviderRetryAt: at})
			if (err == nil) != valid || !valid && (deferred.Persisted || deferred.FilingRecovery != nil) {
				t.Fatalf("deferral accepted trailing document after valid proof: %+v %v", deferred, err)
			}
		})
	}
}
