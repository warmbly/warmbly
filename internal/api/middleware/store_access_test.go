package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// The store's gates as routes.go composes them: reads take READ_STORE, changes
// MANAGE_STORE, and a charge also holds the key's member to manage_billing.
func TestStoreGatesForAPIKeys(t *testing.T) {
	h := &Handler{OrganizationService: &membershipOrgs{}}
	org, other, user := uuid.New(), uuid.New(), uuid.New()
	emails := models.PermManageEmails
	both := models.PermManageEmails | models.PermManageBilling

	read := h.RequireAccess(models.PermManageEmails, models.APIPermReadStore)
	manage := h.RequireAccess(models.PermManageEmails, models.APIPermManageStore)
	billing := h.RequireAccess(models.PermManageBilling, models.APIPermManageStore)

	tests := []struct {
		name    string
		auth    string
		scopes  uint64
		member  models.OrganizationPermission
		memOrg  uuid.UUID
		gates   []gin.HandlerFunc
		allowed bool
	}{
		{"read without READ_STORE", AuthTypeAPIKey, models.APIPermReadEmails | models.APIPermWriteEmails, emails, org, []gin.HandlerFunc{read}, false},
		{"read with READ_STORE", AuthTypeAPIKey, models.APIPermReadStore, emails, org, []gin.HandlerFunc{read}, true},
		{"read with only MANAGE_STORE", AuthTypeAPIKey, models.APIPermManageStore, emails, org, []gin.HandlerFunc{read}, false},
		{"read by a member without manage_emails", AuthTypeAPIKey, models.APIPermReadStore, models.PermViewCampaigns, org, []gin.HandlerFunc{read}, false},
		{"change with READ_STORE only", AuthTypeAPIKey, models.APIPermReadStore, both, org, []gin.HandlerFunc{manage}, false},
		{"change with MANAGE_STORE", AuthTypeAPIKey, models.APIPermManageStore, emails, org, []gin.HandlerFunc{manage}, true},
		{"charge by a member without manage_billing", AuthTypeAPIKey, models.APIPermManageStore, emails, org, []gin.HandlerFunc{manage, billing}, false},
		{"charge by a member with manage_billing", AuthTypeAPIKey, models.APIPermManageStore, both, org, []gin.HandlerFunc{manage, billing}, true},
		{"oauth charge by a member with manage_billing", AuthTypeOAuth, models.APIPermManageStore, both, org, []gin.HandlerFunc{manage, billing}, true},
		{"oauth charge without manage_billing", AuthTypeOAuth, models.APIPermManageStore, emails, org, []gin.HandlerFunc{manage, billing}, false},
		// A membership resolved for another workspace grants nothing in this one.
		{"membership in another workspace", AuthTypeAPIKey, models.APIPermReadStore, both, other, []gin.HandlerFunc{read}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(RequestIDMiddleware(), func(c *gin.Context) {
				c.Set(AuthTypeKey, tt.auth)
				c.Set(APIKeyPermissionsKey, tt.scopes)
				c.Set(UserIDKey, user.String())
				c.Set(OrganizationIDKey, org)
				c.Set(SessionMemberKey, &models.OrganizationMember{OrganizationID: tt.memOrg, UserID: user, Role: "member", Permissions: tt.member})
				c.Next()
			})
			ran := false
			handlers := append(append([]gin.HandlerFunc{}, tt.gates...), func(c *gin.Context) {
				ran = true
				c.Status(http.StatusOK)
			})
			r.GET("/store", handlers...)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/store", nil))
			if ran != tt.allowed {
				t.Fatalf("handler ran = %v, want %v (status %d, body %s)", ran, tt.allowed, w.Code, w.Body.String())
			}
			if !tt.allowed && w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
}

// A charge needs a fresh confirmation from a session, never from a key or a token.
func TestStoreFreshAuthPassesCredentialsOnly(t *testing.T) {
	for _, tt := range []struct {
		auth    string
		allowed bool
	}{{AuthTypeAPIKey, true}, {AuthTypeOAuth, true}, {AuthTypeJWT, false}} {
		r := gin.New()
		r.Use(RequestIDMiddleware(), func(c *gin.Context) {
			c.Set(AuthTypeKey, tt.auth)
			if tt.auth == AuthTypeJWT {
				c.Set(SessionKey, &models.Session{})
			}
			c.Next()
		})
		ran := false
		r.POST("/store/orders", RequireFreshAuth(), func(c *gin.Context) { ran = true })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/store/orders", nil))
		if ran != tt.allowed {
			t.Fatalf("%s: handler ran = %v, want %v (status %d)", tt.auth, ran, tt.allowed, w.Code)
		}
	}
}
