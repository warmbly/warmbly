package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// threadDraftRepo records the upsert and answers with a canned result.
type threadDraftRepo struct {
	repository.AIDraftRepository
	calls   int
	org     uuid.UUID
	params  repository.UpsertDraftParams
	draft   *models.AIThreadDraft
	created bool
	err     error
}

func (r *threadDraftRepo) UpsertPendingDraftForThread(_ context.Context, org uuid.UUID, p repository.UpsertDraftParams) (*models.AIThreadDraft, bool, error) {
	r.calls++
	r.org, r.params = org, p
	return r.draft, r.created, r.err
}

func putThreadDraft(t *testing.T, repo *threadDraftRepo, org uuid.UUID, allowed []uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &Handler{AuditService: audit.NewNoOpService()}
	if repo != nil {
		h.AIDraftRepo = repo
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, uuid.NewString())
		if org != uuid.Nil {
			c.Set(middleware.OrganizationIDKey, org)
		}
		if allowed != nil {
			c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
			c.Set(middleware.APIKeyAllowedEmailAccountsKey, allowed)
		}
	})
	router.PUT("/unibox/ai-drafts/by-thread", h.UpsertThreadDraft)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/unibox/ai-drafts/by-thread", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return out.Code
}

func TestUpsertThreadDraftCreatesAndReplaces(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	src := uuid.New()
	for _, created := range []bool{true, false} {
		repo := &threadDraftRepo{
			draft:   &models.AIThreadDraft{ID: id, EmailAccountID: uuid.New(), ThreadID: "t-1", SourceMessageID: &src},
			created: created,
		}
		rec := putThreadDraft(t, repo, org, nil,
			`{"thread_id":" t-1 ","body":" Thanks, sending the details. ","source":"llcdrop","intent_class":"pricing","confidence":0.8}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("created=%v status %d: %s", created, rec.Code, rec.Body.String())
		}
		var got struct {
			ID      uuid.UUID `json:"id"`
			Status  string    `json:"status"`
			Created bool      `json:"created"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != id || got.Status != "pending" || got.Created != created {
			t.Fatalf("response %+v, want id %s pending created=%v", got, id, created)
		}
		want := repository.UpsertDraftParams{ThreadID: "t-1", Body: "Thanks, sending the details.", Model: "llcdrop", IntentClass: "pricing", Confidence: 0.8}
		if repo.org != org || !reflect.DeepEqual(repo.params, want) {
			t.Fatalf("repo got org %s params %+v, want %+v", repo.org, repo.params, want)
		}
	}
}

func TestUpsertThreadDraftOptionalFieldsDefault(t *testing.T) {
	repo := &threadDraftRepo{draft: &models.AIThreadDraft{ID: uuid.New()}}
	rec := putThreadDraft(t, repo, uuid.New(), nil, `{"thread_id":"t","body":"hi","source":"llcdrop"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if repo.params.IntentClass != "" || repo.params.Confidence != 0 {
		t.Fatalf("defaults not applied: %+v", repo.params)
	}
}

func TestUpsertThreadDraftRejectsBadInput(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	for name, body := range map[string]string{
		"no thread":         `{"body":"hi","source":"llcdrop"}`,
		"blank thread":      `{"thread_id":"  ","body":"hi","source":"llcdrop"}`,
		"thread too long":   `{"thread_id":"` + long(999) + `","body":"hi","source":"llcdrop"}`,
		"no body":           `{"thread_id":"t","source":"llcdrop"}`,
		"blank body":        `{"thread_id":"t","body":" \n ","source":"llcdrop"}`,
		"body too long":     `{"thread_id":"t","body":"` + long(8001) + `","source":"llcdrop"}`,
		"no source":         `{"thread_id":"t","body":"hi"}`,
		"source charset":    `{"thread_id":"t","body":"hi","source":"ll cdrop"}`,
		"source too long":   `{"thread_id":"t","body":"hi","source":"` + long(41) + `"}`,
		"intent too long":   `{"thread_id":"t","body":"hi","source":"llcdrop","intent_class":"` + long(41) + `"}`,
		"confidence high":   `{"thread_id":"t","body":"hi","source":"llcdrop","confidence":1.5}`,
		"confidence low":    `{"thread_id":"t","body":"hi","source":"llcdrop","confidence":-0.1}`,
		"confidence string": `{"thread_id":"t","body":"hi","source":"llcdrop","confidence":"high"}`,
		"thread NUL":        `{"thread_id":"t\u0000x","body":"hi","source":"llcdrop"}`,
		"body NUL":          `{"thread_id":"t","body":"hi\u0000","source":"llcdrop"}`,
		"intent NUL":        `{"thread_id":"t","body":"hi","source":"llcdrop","intent_class":"a\u0000"}`,
		"not json":          `nope`,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &threadDraftRepo{draft: &models.AIThreadDraft{ID: uuid.New()}}
			rec := putThreadDraft(t, repo, uuid.New(), nil, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if repo.calls != 0 {
				t.Fatal("invalid input reached the repository")
			}
		})
	}
}

func TestUpsertThreadDraftAcceptsBoundaryBody(t *testing.T) {
	repo := &threadDraftRepo{draft: &models.AIThreadDraft{ID: uuid.New()}}
	body := `{"thread_id":"t","source":"llcdrop","confidence":1,"body":"` + strings.Repeat("é", 8000) + `"}`
	if rec := putThreadDraft(t, repo, uuid.New(), nil, body); rec.Code != http.StatusOK {
		t.Fatalf("8000 characters refused: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUpsertThreadDraftMapsRepositoryOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"unknown thread", repository.ErrDraftThreadNotFound, http.StatusNotFound, "thread_not_found"},
		{"already sent", repository.ErrDraftAlreadySent, http.StatusConflict, "already_sent"},
		{"discarded", repository.ErrDraftDiscarded, http.StatusConflict, "draft_discarded"},
		{"mailbox outside key", repository.ErrDraftMailboxNotAllowed, http.StatusForbidden, ""},
		{"store failure", context.DeadlineExceeded, http.StatusInternalServerError, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &threadDraftRepo{err: tc.err}
			rec := putThreadDraft(t, repo, uuid.New(), nil, `{"thread_id":"t","body":"hi","source":"llcdrop"}`)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.code != "" && errCode(t, rec) != tc.code {
				t.Fatalf("code %q, want %q", errCode(t, rec), tc.code)
			}
			if strings.Contains(rec.Body.String(), "deadline") {
				t.Fatalf("store error leaked: %s", rec.Body.String())
			}
		})
	}
}

func TestUpsertThreadDraftHonoursMailboxAllowlist(t *testing.T) {
	box := uuid.New()
	repo := &threadDraftRepo{draft: &models.AIThreadDraft{ID: uuid.New()}}
	if rec := putThreadDraft(t, repo, uuid.New(), []uuid.UUID{box}, `{"thread_id":"t","body":"hi","source":"llcdrop"}`); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(repo.params.AllowedMailboxes, []uuid.UUID{box}) {
		t.Fatalf("allowlist not passed to the repository: %v", repo.params.AllowedMailboxes)
	}
	repo = &threadDraftRepo{draft: &models.AIThreadDraft{ID: uuid.New()}}
	putThreadDraft(t, repo, uuid.New(), nil, `{"thread_id":"t","body":"hi","source":"llcdrop"}`)
	if repo.params.AllowedMailboxes != nil {
		t.Fatalf("unrestricted caller got an allowlist: %v", repo.params.AllowedMailboxes)
	}
}

func TestUpsertThreadDraftNeedsOrganizationAndRepo(t *testing.T) {
	body := `{"thread_id":"t","body":"hi","source":"llcdrop"}`
	repo := &threadDraftRepo{draft: &models.AIThreadDraft{ID: uuid.New()}}
	if rec := putThreadDraft(t, repo, uuid.Nil, nil, body); rec.Code != http.StatusBadRequest || repo.calls != 0 {
		t.Fatalf("no organization: status %d calls %d", rec.Code, repo.calls)
	}
	if rec := putThreadDraft(t, nil, uuid.New(), nil, body); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no draft store: status %d", rec.Code)
	}
}

// approveDraftRepo answers a claim with a canned snapshot while GetDraft holds an older version.
type approveDraftRepo struct {
	repository.AIDraftRepository
	stale    *models.AIThreadDraft
	claimed  *models.AIThreadDraft
	allowed  []uuid.UUID
	reverted bool
}

func (r *approveDraftRepo) GetDraft(context.Context, uuid.UUID, uuid.UUID) (*models.AIThreadDraft, error) {
	return r.stale, nil
}

func (r *approveDraftRepo) ClaimDraftForApproval(_ context.Context, _, _ uuid.UUID, allowed []uuid.UUID) (*models.AIThreadDraft, error) {
	r.allowed = allowed
	return r.claimed, nil
}

func (r *approveDraftRepo) RevertApprovedToPending(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	r.reverted = true
	return true, nil
}

// recordingSender captures the one send it is asked for.
type recordingSender struct {
	account uuid.UUID
	req     *emailsend.SendEmailRequest
	err     *errx.Error
}

func (s *recordingSender) SendEmail(_ context.Context, _, _, accountID uuid.UUID, req *emailsend.SendEmailRequest) (*emailsend.SendEmailResponse, *errx.Error) {
	s.account, s.req = accountID, req
	if s.err != nil {
		return nil, s.err
	}
	return &emailsend.SendEmailResponse{}, nil
}

func approveDraft(t *testing.T, repo *approveDraftRepo, sender *recordingSender, allowed []uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &Handler{AuditService: audit.NewNoOpService(), AIDraftRepo: repo, EmailSendService: sender}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, uuid.NewString())
		c.Set(middleware.OrganizationIDKey, uuid.New())
		if allowed != nil {
			c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
			c.Set(middleware.APIKeyAllowedEmailAccountsKey, allowed)
		}
	})
	router.POST("/unibox/agent-drafts/:id/approve", h.ApproveAgentDraft)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/unibox/agent-drafts/"+uuid.NewString()+"/approve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestApproveAgentDraftSendsTheClaimedSnapshot(t *testing.T) {
	id := uuid.New()
	stale := &models.AIThreadDraft{ID: id, EmailAccountID: uuid.New(), ThreadID: "t", ToAddr: "old@lead.test", Subject: "Re: old", InReplyTo: "<old@lead.test>", Body: "old", Status: "pending"}
	claimed := &models.AIThreadDraft{ID: id, EmailAccountID: uuid.New(), ThreadID: "t", ToAddr: "new@lead.test", Subject: "Re: new", InReplyTo: "<new@lead.test>", Body: "new", Status: "approved"}

	repo := &approveDraftRepo{stale: stale, claimed: claimed}
	sender := &recordingSender{}
	if rec := approveDraft(t, repo, sender, nil, `{}`); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	want := &emailsend.SendEmailRequest{To: []string{"new@lead.test"}, Subject: "Re: new", BodyPlain: "new", ThreadID: "t", SendMode: "instant", InReplyTo: []string{"<new@lead.test>"}}
	if sender.account != claimed.EmailAccountID || !reflect.DeepEqual(sender.req, want) {
		t.Fatalf("sent %+v from %s, want the claimed snapshot %+v from %s", sender.req, sender.account, want, claimed.EmailAccountID)
	}
	if repo.allowed != nil {
		t.Fatalf("unrestricted caller claimed with an allowlist: %v", repo.allowed)
	}

	// An edited body replaces only the body; addressing stays the claimed row's.
	sender = &recordingSender{}
	approveDraft(t, &approveDraftRepo{stale: stale, claimed: claimed}, sender, nil, `{"body":" edited "}`)
	if sender.req.BodyPlain != "edited" || sender.req.To[0] != "new@lead.test" {
		t.Fatalf("edited send = %+v", sender.req)
	}

	// A failed send returns the claimed draft to pending.
	repo = &approveDraftRepo{stale: stale, claimed: claimed}
	approveDraft(t, repo, &recordingSender{err: errx.New(errx.BadRequest, "nope")}, nil, `{}`)
	if !repo.reverted {
		t.Fatal("a failed send left the draft approved")
	}
}

func TestApproveAgentDraftExplainsARefusedClaim(t *testing.T) {
	box := uuid.New()
	for _, tc := range []struct {
		name    string
		stale   *models.AIThreadDraft
		allowed []uuid.UUID
		status  int
	}{
		{"missing", nil, nil, http.StatusNotFound},
		{"already handled", &models.AIThreadDraft{EmailAccountID: box, Status: "approved"}, nil, http.StatusConflict},
		{"outside the key's mailboxes", &models.AIThreadDraft{EmailAccountID: box, Status: "pending"}, []uuid.UUID{uuid.New()}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &approveDraftRepo{stale: tc.stale}
			sender := &recordingSender{}
			rec := approveDraft(t, repo, sender, tc.allowed, `{}`)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if sender.req != nil {
				t.Fatal("a refused claim sent mail")
			}
			if tc.allowed != nil && !reflect.DeepEqual(repo.allowed, tc.allowed) {
				t.Fatalf("claim allowlist = %v, want %v", repo.allowed, tc.allowed)
			}
		})
	}
}
