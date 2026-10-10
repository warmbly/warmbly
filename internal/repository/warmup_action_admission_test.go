package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
