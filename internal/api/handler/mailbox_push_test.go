package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/mailpush"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type pushTargets struct {
	repository.MailboxPushRepository
}

func (pushTargets) WakeTargets(_ context.Context, ids []uuid.UUID) ([]repository.SyncWakeTarget, error) {
	out := make([]repository.SyncWakeTarget, 0, len(ids))
	for _, id := range ids {
		out = append(out, repository.SyncWakeTarget{EmailID: id, WorkerID: uuid.New()})
	}
	return out, nil
}

type pushWakes struct{ got []models.SyncWake }

func (p *pushWakes) PublishSyncWake(_ context.Context, _ uuid.UUID, w *models.SyncWake) error {
	p.got = append(p.got, *w)
	return nil
}

func TestMailboxPushGraph(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GRAPH_PUSH", "true")
	t.Setenv("BACKEND_PUBLIC_URL", "https://api.example.com")
	settings := mailpush.NewSettings("an-auth-secret-of-at-least-32-bytes!!", nil)
	wakes := &pushWakes{}
	h := &Handler{MailPush: settings, MailPushWaker: mailpush.NewWaker(pushTargets{}, wakes, nil)}
	r := gin.New()
	r.POST("/api/v1/mailbox-push/graph/:emailId", h.MailboxPushGraph)
	id := uuid.New()
	post := func(query, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mailbox-push/graph/"+id.String()+query, strings.NewReader(body))
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("?validationToken=abc%20123", ""); rec.Code != http.StatusOK || rec.Body.String() != "abc 123" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("validation answered %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if rec := post("", `{"value":[{"clientState":"forged","changeType":"created"}]}`); rec.Code != http.StatusAccepted || len(wakes.got) != 0 {
		t.Fatalf("a forged notification answered %d and woke %d", rec.Code, len(wakes.got))
	}
	body := `{"value":[{"clientState":"` + settings.GraphClientState(id) + `","lifecycleEvent":"reauthorizationRequired"}]}`
	if rec := post("", body); rec.Code != http.StatusAccepted || len(wakes.got) != 1 || wakes.got[0].EmailID != id || !wakes.got[0].Renew {
		t.Fatalf("an authentic lifecycle event answered %d, wakes %+v", rec.Code, wakes.got)
	}
}

func TestMailboxPushGmailRefusesUnsignedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("BACKEND_PUBLIC_URL", "https://api.example.com")
	t.Setenv("GMAIL_PUSH_SERVICE_ACCOUNT", "push@p.iam.gserviceaccount.com")
	wakes := &pushWakes{}
	post := func(h *Handler, auth string) int {
		r := gin.New()
		r.POST("/api/v1/mailbox-push/gmail", h.MailboxPushGmail)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mailbox-push/gmail", strings.NewReader(`{"message":{"data":"e30="}}`))
		req.Header.Set("Authorization", auth)
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(&Handler{MailPushWaker: mailpush.NewWaker(pushTargets{}, wakes, nil)}, "Bearer x"); code != http.StatusNotFound {
		t.Fatalf("unconfigured endpoint answered %d", code)
	}
	h := &Handler{GmailPush: mailpush.NewGmailPush(), MailPushWaker: mailpush.NewWaker(pushTargets{}, wakes, nil)}
	for _, auth := range []string{"", "Bearer not-a-jwt", "Basic abc"} {
		if code := post(h, auth); code != http.StatusUnauthorized {
			t.Fatalf("authorization %q answered %d", auth, code)
		}
	}
	if len(wakes.got) != 0 {
		t.Fatal("an unsigned request woke a mailbox")
	}
}
