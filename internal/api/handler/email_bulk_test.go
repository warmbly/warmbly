package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type bulkEmailStub struct {
	email.EmailService
	orgID uuid.UUID
	fail  map[string]*errx.Error

	mu    sync.Mutex
	calls []string
}

func (s *bulkEmailStub) record(t *testing.T, orgID, id string) *errx.Error {
	if orgID != s.orgID.String() {
		t.Errorf("operation ran in org %s, want %s", orgID, s.orgID)
	}
	s.mu.Lock()
	s.calls = append(s.calls, id)
	s.mu.Unlock()
	return s.fail[id]
}

type bulkEmailStubT struct {
	*bulkEmailStub
	t *testing.T
}

func (s bulkEmailStubT) Delete(_ context.Context, orgID, id string) *errx.Error {
	return s.record(s.t, orgID, id)
}

func (s bulkEmailStubT) SetWarmupLifecycle(_ context.Context, orgID, id, _ string) (*models.Email, *errx.Error) {
	return &models.Email{}, s.record(s.t, orgID, id)
}

func (s bulkEmailStubT) Update(_ context.Context, orgID, _, id string, u *models.UpdateEmail) (*models.Email, *errx.Error) {
	if u.Status == nil || *u.Status != "inactive" {
		s.t.Errorf("update carried status %v, want inactive", u.Status)
	}
	return &models.Email{}, s.record(s.t, orgID, id)
}

func serveBulk(t *testing.T, stub *bulkEmailStub, allowlist []uuid.UUID, path, body string) (*httptest.ResponseRecorder, bulkMailboxResponse) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &Handler{EmailService: bulkEmailStubT{stub, t}}
	router := gin.New()
	router.POST("/emails/bulk/:op", func(c *gin.Context) {
		c.Set(middleware.OrganizationIDKey, stub.orgID)
		if allowlist != nil {
			c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
			c.Set(middleware.APIKeyAllowedEmailAccountsKey, allowlist)
		}
		switch c.Param("op") {
		case "delete":
			h.BulkDeleteEmails(c)
		case "warmup":
			h.BulkWarmupEmails(c)
		case "status":
			h.BulkStatusEmails(c)
		}
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	var res bulkMailboxResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
	}
	return rec, res
}

func idsJSON(ids ...uuid.UUID) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

func TestBulkMailboxesReportEachOutcomeInOrder(t *testing.T) {
	org, a, b, c := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	stub := &bulkEmailStub{orgID: org, fail: map[string]*errx.Error{
		b.String(): errx.NewWithIdentifier(errx.ServiceUnavailable, "mailbox_worker_unreachable", "The machine syncing this mailbox could not be reached."),
		c.String(): errx.New(errx.Internal, "pq: relation exploded"),
	}}
	rec, res := serveBulk(t, stub, nil, "/emails/bulk/delete", `{"email_ids":`+idsJSON(a, b, a, c)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if res.Succeeded != 1 || res.Failed != 2 || len(res.Results) != 3 {
		t.Fatalf("summary = %+v", res)
	}
	if len(stub.calls) != 3 {
		t.Fatalf("a duplicate id ran twice: %v", stub.calls)
	}
	want := []BulkMailboxResult{
		{ID: a, OK: true},
		{ID: b, Code: "mailbox_worker_unreachable", Message: "The machine syncing this mailbox could not be reached."},
		{ID: c, Code: "internal_error", Message: "Something went wrong."},
	}
	for i, w := range want {
		if res.Results[i] != w {
			t.Fatalf("result %d = %+v, want %+v", i, res.Results[i], w)
		}
	}
}

func TestBulkMailboxesHoldToTheKeyAllowlistBeforeActing(t *testing.T) {
	org, allowed, outside := uuid.New(), uuid.New(), uuid.New()
	stub := &bulkEmailStub{orgID: org}
	rec, _ := serveBulk(t, stub, []uuid.UUID{allowed}, "/emails/bulk/status", `{"status":"inactive","email_ids":`+idsJSON(allowed, outside)+`}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("mailboxes were changed before the allowlist refused: %v", stub.calls)
	}
	rec, res := serveBulk(t, stub, []uuid.UUID{allowed}, "/emails/bulk/status", `{"status":"inactive","email_ids":`+idsJSON(allowed)+`}`)
	if rec.Code != http.StatusOK || res.Succeeded != 1 {
		t.Fatalf("status %d, %+v", rec.Code, res)
	}
}

func TestBulkMailboxesRefuseBadRequests(t *testing.T) {
	org := uuid.New()
	tooMany := make([]uuid.UUID, BulkMailboxMax+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}
	for name, tc := range map[string]struct{ path, body string }{
		"no ids":         {"/emails/bulk/delete", `{"email_ids":[]}`},
		"too many":       {"/emails/bulk/delete", `{"email_ids":` + idsJSON(tooMany...) + `}`},
		"not a uuid":     {"/emails/bulk/delete", `{"email_ids":["nope"]}`},
		"unknown action": {"/emails/bulk/warmup", `{"action":"boost","email_ids":` + idsJSON(uuid.New()) + `}`},
		"unknown status": {"/emails/bulk/status", `{"status":"revoked","email_ids":` + idsJSON(uuid.New()) + `}`},
	} {
		t.Run(name, func(t *testing.T) {
			stub := &bulkEmailStub{orgID: org}
			rec, _ := serveBulk(t, stub, nil, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
			}
			if len(stub.calls) != 0 {
				t.Fatalf("a refused request changed mailboxes: %v", stub.calls)
			}
		})
	}
}

func TestBulkWarmupPauseRunsEveryMailbox(t *testing.T) {
	org, a, b := uuid.New(), uuid.New(), uuid.New()
	stub := &bulkEmailStub{orgID: org}
	rec, res := serveBulk(t, stub, nil, "/emails/bulk/warmup", `{"action":"pause","email_ids":`+idsJSON(a, b)+`}`)
	if rec.Code != http.StatusOK || res.Succeeded != 2 || res.Failed != 0 {
		t.Fatalf("status %d, %+v", rec.Code, res)
	}
}
