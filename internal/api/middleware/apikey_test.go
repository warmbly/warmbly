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

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequireEmailAccountParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allowedID := uuid.New()
	deniedID := uuid.New()

	tests := []struct {
		name       string
		authType   string
		allowlist  []uuid.UUID
		pathID     uuid.UUID
		wantStatus int
	}{
		{"jwt bypasses allowlist", AuthTypeJWT, []uuid.UUID{allowedID}, deniedID, http.StatusOK},
		{"unrestricted key bypasses allowlist", AuthTypeAPIKey, nil, deniedID, http.StatusOK},
		{"allowed key passes", AuthTypeAPIKey, []uuid.UUID{allowedID}, allowedID, http.StatusOK},
		{"denied key fails", AuthTypeAPIKey, []uuid.UUID{allowedID}, deniedID, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(RequestIDMiddleware())
			r.GET("/emails/:id",
				func(c *gin.Context) {
					c.Set(AuthTypeKey, tt.authType)
					c.Set(APIKeyAllowedEmailAccountsKey, tt.allowlist)
					c.Next()
				},
				RequireEmailAccountParam("id"),
				func(c *gin.Context) {
					c.Status(http.StatusOK)
				},
			)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/emails/"+tt.pathID.String(), nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRequireAPIPermission(t *testing.T) {
	tests := []struct {
		name       string
		authType   string
		granted    uint64
		required   uint64
		wantStatus int
	}{
		{"jwt-bypass", AuthTypeJWT, 0, models.APIPermReadCampaigns, http.StatusOK},
		{"apikey-granted", AuthTypeAPIKey, models.APIPermReadCampaigns | models.APIPermReadContacts, models.APIPermReadCampaigns, http.StatusOK},
		{"apikey-missing", AuthTypeAPIKey, models.APIPermReadContacts, models.APIPermReadCampaigns, http.StatusForbidden},
		{"apikey-no-perms-key", AuthTypeAPIKey, 0, models.APIPermReadCampaigns, http.StatusForbidden},
		{"unauthenticated", "", 0, models.APIPermReadCampaigns, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tt.authType != "" {
					c.Set(AuthTypeKey, tt.authType)
				}
				if tt.authType == AuthTypeAPIKey && tt.granted != 0 {
					c.Set(APIKeyPermissionsKey, tt.granted)
				}
				c.Next()
			})
			r.GET("/x", RequireAPIPermission(tt.required), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestRequireAccessAPIKeyPath(t *testing.T) {
	// JWT path is exercised in handler-level integration tests; here we
	// pin the API-key branch since it doesn't depend on OrganizationService.
	h := &Handler{}

	tests := []struct {
		name       string
		granted    uint64
		required   uint64
		wantStatus int
	}{
		{"granted", models.APIPermWriteCampaigns | models.APIPermReadAnalytics, models.APIPermWriteCampaigns, http.StatusOK},
		{"missing", models.APIPermReadAnalytics, models.APIPermWriteCampaigns, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(AuthTypeKey, AuthTypeAPIKey)
				c.Set(APIKeyPermissionsKey, tt.granted)
				c.Next()
			})
			r.GET("/x", h.RequireAccess(models.PermManageCampaigns, tt.required), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// The unibox gate on /contacts/lookup applies only when thread_id is present.
func TestRequireAccessWithQueryOnlyGatesWithTheParam(t *testing.T) {
	h := &Handler{}
	tests := []struct {
		name       string
		url        string
		granted    uint64
		wantStatus int
	}{
		{"no param, no unibox scope", "/x?email=a@b.test", models.APIPermReadContacts, http.StatusOK},
		{"param, no unibox scope", "/x?thread_id=t1", models.APIPermReadContacts, http.StatusForbidden},
		{"blank param", "/x?thread_id=%20", models.APIPermReadContacts, http.StatusOK},
		{"param, unibox scope", "/x?thread_id=t1", models.APIPermReadContacts | models.APIPermReadUnibox, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(AuthTypeKey, AuthTypeAPIKey)
				c.Set(APIKeyPermissionsKey, tt.granted)
				c.Next()
			})
			calls := 0
			r.GET("/x", h.RequireAccessWithQuery("thread_id", models.PermAccessUnibox, models.APIPermReadUnibox), func(c *gin.Context) {
				calls++
				c.JSON(http.StatusOK, gin.H{})
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.url, nil))
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if want := map[bool]int{true: 1, false: 0}[tt.wantStatus == http.StatusOK]; calls != want {
				t.Errorf("handler ran %d times, want %d", calls, want)
			}
		})
	}
}

// A session caller is refused when no organization service is wired to check it.
func TestOrgGatesRefuseWithoutOrganizationService(t *testing.T) {
	h := &Handler{}
	for name, gate := range map[string]gin.HandlerFunc{
		"RequireAccess":     h.RequireAccess(models.PermManageSettings, models.APIPermIntegrations),
		"RequireAnyAccess":  h.RequireAnyAccess(models.APIPermIntegrations, models.PermManageSettings, models.PermUseIntegrations),
		"RequirePermission": h.RequirePermission(models.PermManageSettings),
	} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set(AuthTypeKey, AuthTypeJWT)
			c.Next()
		})
		calls := 0
		r.GET("/x", gate, func(c *gin.Context) {
			calls++
			c.JSON(http.StatusOK, gin.H{})
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil))
		if w.Code != http.StatusInternalServerError || calls != 0 {
			t.Errorf("%s: status = %d, handler ran %d times; want 500 and 0", name, w.Code, calls)
		}
	}
}
