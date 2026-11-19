package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/models"
)

// Auto-renew changes what is charged: the member needs manage_billing, and only
// a session has a recent confirmation to show; a key or token passes that part.
func TestStoreAutoRenewNeedsBillingAndASessionConfirmation(t *testing.T) {
	org, user := uuid.New(), uuid.New()
	tests := []struct {
		name string
		auth string
		perm models.OrganizationPermission
		want string // the refusal's code, or store_disabled once the gates let it through
	}{
		{"key without manage_billing", middleware.AuthTypeAPIKey, models.PermManageEmails, "forbidden"},
		{"key with manage_billing", middleware.AuthTypeAPIKey, models.PermManageEmails | models.PermManageBilling, "store_disabled"},
		{"oauth with manage_billing", middleware.AuthTypeOAuth, models.PermManageEmails | models.PermManageBilling, "store_disabled"},
		{"session not confirmed recently", middleware.AuthTypeJWT, models.PermManageEmails | models.PermManageBilling, "reauth_required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{}
			r := gin.New()
			r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
				c.Set(middleware.AuthTypeKey, tt.auth)
				c.Set(middleware.UserIDKey, user.String())
				c.Set(middleware.OrganizationIDKey, org)
				c.Set(middleware.SessionMemberKey, &models.OrganizationMember{OrganizationID: org, UserID: user, Role: "member", Permissions: tt.perm})
				if tt.auth == middleware.AuthTypeJWT {
					c.Set(middleware.SessionKey, &models.Session{})
				}
				c.Next()
			})
			r.PATCH("/store/domains/:id", h.PatchStoreDomain)
			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/store/domains/"+uuid.NewString(), strings.NewReader(`{"auto_renew":false}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			var body struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			got := body.Code
			if got == "" && w.Code == http.StatusForbidden {
				got = "forbidden"
			}
			if got != tt.want {
				t.Fatalf("code = %q (status %d, body %s), want %q", got, w.Code, w.Body.String(), tt.want)
			}
		})
	}
}
