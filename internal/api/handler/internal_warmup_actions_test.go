package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

type warmupDeferralAuthority struct {
	repository.WarmupDispatchRepository
	req   models.WarmupFilingDeferralRequest
	out   models.WarmupFilingDeferralDecision
	err   error
	calls int
}

func (a *warmupDeferralAuthority) DeferWarmupFiling(_ context.Context, req models.WarmupFilingDeferralRequest) (models.WarmupFilingDeferralDecision, error) {
	a.req, a.calls = req, a.calls+1
	return a.out, a.err
}

func TestInternalWarmupFilingDeferralRequiresScopedMutation(t *testing.T) {
	req := models.WarmupFilingDeferralRequest{MailboxID: uuid.New(), WorkerID: uuid.New(), FilingID: uuid.New(), ProviderRetryAt: time.Now().Add(time.Hour)}
	for _, tc := range []struct {
		name   string
		method string
		input  models.WarmupFilingDeferralRequest
		err    error
		legacy bool
		want   int
		calls  int
	}{
		{"committed", http.MethodPost, req, nil, false, http.StatusOK, 1},
		{"read refused", http.MethodGet, req, nil, false, http.StatusNotFound, 0},
		{"invalid scope", http.MethodPost, models.WarmupFilingDeferralRequest{MailboxID: uuid.Nil}, nil, false, http.StatusBadRequest, 0},
		{"unavailable", http.MethodPost, req, errors.New("database unavailable"), false, http.StatusServiceUnavailable, 1},
		{"stale work", http.MethodPost, req, repository.ErrSendAdmissionDenied, false, http.StatusForbidden, 1},
		{"unsupported backend", http.MethodPost, req, nil, true, http.StatusServiceUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &warmupDeferralAuthority{err: tc.err, out: models.WarmupFilingDeferralDecision{Persisted: true, FilingRecovery: &models.WarmupFilingRecoveryProof{Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: req.FilingID, ProviderRetryAt: &req.ProviderRetryAt}}}
			h := Handler{WarmupDispatch: stub}
			if tc.legacy {
				h.WarmupDispatch = &legacyWarmupAuthority{}
			}
			r := gin.New()
			r.POST("/api/v1/internal/worker/warmup-actions/defer", h.InternalWarmupFilingDeferral)
			body, _ := json.Marshal(tc.input)
			httpRequest := httptest.NewRequest(tc.method, "/api/v1/internal/worker/warmup-actions/defer", strings.NewReader(string(body)))
			httpRequest.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			r.ServeHTTP(out, httpRequest)
			if out.Code != tc.want || stub.calls != tc.calls || out.Header().Get("Cache-Control") != map[bool]string{true: "", false: "no-store"}[tc.method == http.MethodGet] {
				t.Fatalf("deferral status=%d calls=%d headers=%v", out.Code, stub.calls, out.Header())
			}
			if out.Code == http.StatusOK && (!stub.req.ProviderRetryAt.Equal(req.ProviderRetryAt) || stub.req.WorkerID != req.WorkerID || stub.req.FilingID != req.FilingID) {
				t.Fatalf("deferral request lost scope: %+v", stub.req)
			}
		})
	}
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
