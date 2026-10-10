package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupRecoveryAuthority struct {
	repository.WarmupDispatchRepository
	request models.WarmupActionRequest
	err     error
}

func (r *warmupRecoveryAuthority) AdmitWarmupAction(_ context.Context, req models.WarmupActionRequest) (models.WarmupActionDecision, error) {
	r.request = req
	return models.WarmupActionDecision{Actions: req.Actions, FilingPending: req.FilingID != ""}, r.err
}

type legacyWarmupAuthority struct {
	repository.WarmupDispatchRepository
}

func (r *legacyWarmupAuthority) PermittedWarmupActions(_ context.Context, _, _ uuid.UUID, actions []string) ([]string, error) {
	return actions, nil
}

func TestInternalWarmupFilingAdmissionCompatibility(t *testing.T) {
	request := models.WarmupActionRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), Actions: []string{models.WarmupActionFile}, FilingID: uuid.NewString()}
	for _, tc := range []struct {
		name      string
		authority repository.WarmupDispatchRepository
		status    int
		pending   bool
	}{
		{"durable authority", &warmupRecoveryAuthority{}, http.StatusOK, true},
		{"legacy authority", &legacyWarmupAuthority{}, http.StatusOK, false},
		{"failed authority", &warmupRecoveryAuthority{err: errors.New("database unavailable")}, http.StatusServiceUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(request)
			out := httptest.NewRecorder()
			httpRequest := httptest.NewRequest(http.MethodPost, "/internal/worker/warmup-actions", strings.NewReader(string(body)))
			httpRequest.Header.Set("Content-Type", "application/json")
			h := Handler{WarmupDispatch: tc.authority}
			router := gin.New()
			router.POST("/internal/worker/warmup-actions", h.InternalWarmupActions)
			router.ServeHTTP(out, httpRequest)
			if out.Code != tc.status {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if out.Code == http.StatusOK {
				var decision models.WarmupActionDecision
				if err := json.Unmarshal(out.Body.Bytes(), &decision); err != nil || decision.FilingPending != tc.pending || len(decision.Actions) != 1 {
					t.Fatalf("decision=%+v err=%v", decision, err)
				}
			}
			if r, ok := tc.authority.(*warmupRecoveryAuthority); ok && r.request.FilingID != request.FilingID {
				t.Fatal("filing binding lost")
			}
		})
	}
}

func TestInternalWarmupFilingAdmissionRejectsInvalidIdentifier(t *testing.T) {
	for _, id := range []string{"invalid", uuid.Nil.String()} {
		r := &warmupRecoveryAuthority{}
		body, _ := json.Marshal(models.WarmupActionRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), Actions: []string{models.WarmupActionFile}, FilingID: id})
		out := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/internal/worker/warmup-actions", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		h := Handler{WarmupDispatch: r}
		router := gin.New()
		router.POST("/internal/worker/warmup-actions", h.InternalWarmupActions)
		router.ServeHTTP(out, request)
		if out.Code != http.StatusBadRequest || r.request.FilingID != "" {
			t.Fatal("invalid filing reached recovery authority")
		}
	}
}
