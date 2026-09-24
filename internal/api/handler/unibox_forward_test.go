package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type fakeForwardUnibox struct {
	unibox.UniboxService
	mailbox uuid.UUID
	read    int
}

func (f *fakeForwardUnibox) ForwardSource(context.Context, uuid.UUID, uuid.UUID) (*models.ForwardedMessage, *errx.Error) {
	f.read++
	return &models.ForwardedMessage{EmailID: f.mailbox, Subject: "Pricing"}, nil
}

type fakeForwardSend struct{ req *emailsend.SendEmailRequest }

func (f *fakeForwardSend) SendEmail(_ context.Context, _, _, _ uuid.UUID, req *emailsend.SendEmailRequest) (*emailsend.SendEmailResponse, *errx.Error) {
	f.req = req
	return &emailsend.SendEmailResponse{SendMode: "instant"}, nil
}

// Forwarding discloses a stored message, so a key that cannot read the inbox
// is refused before the message is read, and a restricted key must be allowed
// the message's mailbox as well as the sending one.
func TestUniboxReplyForwardNeedsReadAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sender, source := uuid.New(), uuid.New()

	for _, tc := range []struct {
		name    string
		perms   uint64
		allowed []uuid.UUID
		status  int
		read    bool
	}{
		{"write-only key", models.APIPermWriteUnibox, nil, http.StatusForbidden, false},
		{"read and write key", models.APIPermWriteUnibox | models.APIPermReadUnibox, nil, http.StatusOK, true},
		{"key not allowed the source mailbox", models.APIPermWriteUnibox | models.APIPermReadUnibox, []uuid.UUID{sender}, http.StatusForbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ub := &fakeForwardUnibox{mailbox: source}
			send := &fakeForwardSend{}
			h := &Handler{UniboxService: ub, EmailSendService: send, AuditService: audit.NewNoOpService()}

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(middleware.UserIDKey, uuid.NewString())
				c.Set(middleware.OrganizationIDKey, uuid.New())
				c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
				c.Set(middleware.APIKeyPermissionsKey, tc.perms)
				if tc.allowed != nil {
					c.Set(middleware.APIKeyAllowedEmailAccountsKey, tc.allowed)
				}
			})
			router.POST("/unibox/reply", h.UniboxReply)

			body := `{"email_account_id":"` + sender.String() + `","to":["new@example.com"],"subject":"Fwd: Pricing","forward_message_id":"` + uuid.NewString() + `"}`
			req := httptest.NewRequest(http.MethodPost, "/unibox/reply", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if (ub.read > 0) != tc.read {
				t.Fatalf("message read %d times, want read=%v", ub.read, tc.read)
			}
			if tc.status == http.StatusOK && (send.req == nil || send.req.Forward == nil || send.req.Forward.Subject != "Pricing") {
				t.Fatalf("forward did not reach the send: %+v", send.req)
			}
			if tc.status != http.StatusOK && send.req != nil {
				t.Fatal("a refused forward was sent")
			}
		})
	}
}
