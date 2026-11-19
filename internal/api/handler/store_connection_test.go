package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type adminBits struct {
	organization.OrganizationService
	perms uint32
}

func (a adminBits) GetUserAdminPermissions(context.Context, uuid.UUID) (uint32, error) {
	return a.perms, nil
}

// nopStoreRepo is a store repository nothing here reads.
type nopStoreRepo struct{ repository.StoreRepository }

// storeConns keeps a connection in memory and reports nothing live.
type storeConns struct{ row *repository.StoreConnection }

func (s *storeConns) CanSeal() bool { return true }
func (s *storeConns) GetConnection(context.Context) (*repository.StoreConnection, error) {
	return s.row, nil
}
func (s *storeConns) PutConnection(_ context.Context, c repository.StoreConnection) error {
	s.row = &c
	return nil
}
func (s *storeConns) DeleteConnection(context.Context) error { s.row = nil; return nil }
func (s *storeConns) CountLivePurchases(context.Context, *uuid.UUID) (int, int, error) {
	return 0, 0, nil
}

func storeStatusAs(t *testing.T, h *Handler, apiKey bool) store.Status {
	t.Helper()
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
		c.Set(middleware.OrganizationIDKey, uuid.New())
		c.Set(middleware.UserIDKey, uuid.NewString())
		if apiKey {
			c.Set(middleware.APIKeyIDKey, uuid.NewString())
		}
		c.Next()
	})
	r.GET("/store/status", h.GetStoreStatus)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/store/status", nil))
	var st store.Status
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &st) != nil {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	return st
}

func TestStoreStatusOffersSetupToAnInstanceAdminOnly(t *testing.T) {
	t.Setenv("ADMIN_URL", "https://admin.example.com")
	svc := store.New(store.Deps{SelfHosted: true})
	settings := uint32(models.AdminPermManageSettings)

	st := storeStatusAs(t, &Handler{StoreService: svc, OrganizationService: adminBits{perms: settings}}, false)
	if st.Reason != store.ReasonNotConfigured || st.Configured || !st.Locked || !st.CanConfigure || st.ConfigureURL != "https://admin.example.com/health?tab=integrations" {
		t.Fatalf("instance admin: %+v", st)
	}
	for name, h := range map[string]*Handler{
		"member":            {StoreService: svc, OrganizationService: adminBits{}},
		"other admin bit":   {StoreService: svc, OrganizationService: adminBits{perms: uint32(models.AdminPermViewAnalytics)}},
		"no org service":    {StoreService: svc},
		"api key of admins": {StoreService: svc, OrganizationService: adminBits{perms: settings}},
	} {
		if st := storeStatusAs(t, h, name == "api key of admins"); st.CanConfigure || st.ConfigureURL != "" || st.Reason != store.ReasonNotConfigured {
			t.Fatalf("%s: %+v", name, st)
		}
	}
}

func TestStoreStatusKeepsAnUnconfiguredStoreHiddenOnAHostedInstance(t *testing.T) {
	t.Setenv("ADMIN_URL", "https://admin.example.com")
	admin := adminBits{perms: uint32(models.AdminPermManageSettings)}
	for _, selfHosted := range []bool{false, true} {
		h := &Handler{StoreService: store.New(store.Deps{SelfHosted: selfHosted}), OrganizationService: admin}
		st := storeStatusAs(t, h, false)
		// Hosted: not locked, and no setup offer even to an instance admin; the admin panel's card is where they add the key.
		if st.Enabled || st.Reason != store.ReasonNotConfigured || st.Locked != selfHosted || st.CanConfigure != selfHosted || (st.ConfigureURL != "") != selfHosted {
			t.Fatalf("self-hosted %v: %+v", selfHosted, st)
		}
	}
}

// serveAdminConnection registers the connection routes behind the same gates routes.go puts them behind.
func serveAdminConnection(h *Handler, perms models.AdminPermission, fresh bool, method, body string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), func(c *gin.Context) {
		admin := uuid.New()
		session := &models.Session{UserID: admin}
		if fresh {
			now := time.Now()
			session.ReauthAt = &now
		}
		c.Set(middleware.SessionKey, session)
		c.Set(middleware.AdminPermissionsKey, perms)
		c.Set(middleware.AdminUserIDKey, admin)
		c.Next()
	})
	gate := middleware.RequireAdminPermission(models.AdminPermManageSettings)
	r.GET("/admin/store/connection", gate, h.AdminGetStoreConnection)
	r.PUT("/admin/store/connection", gate, middleware.RequireFreshAuth(), h.AdminPutStoreConnection)
	r.POST("/admin/store/connection", gate, middleware.RequireFreshAuth(), h.AdminTestStoreConnection)
	r.DELETE("/admin/store/connection", gate, middleware.RequireFreshAuth(), h.AdminDeleteStoreConnection)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), method, "/admin/store/connection", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAdminStoreConnectionIsGatedAndNeverReturnsASecret(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	svc := store.New(store.Deps{Repo: nopStoreRepo{}})
	conns := &storeConns{}
	svc.WireConnectionSettings(conns, config.GraphKit{})
	h := &Handler{StoreService: svc}
	save := `{"api_url":"fake","webhook_secret":"whsec_hidden_value"}`

	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete} {
		if w := serveAdminConnection(h, 0, true, m, save); w.Code != http.StatusForbidden {
			t.Fatalf("%s without admin: %d", m, w.Code)
		}
		if w := serveAdminConnection(h, models.AdminPermViewAnalytics, true, m, save); w.Code != http.StatusForbidden {
			t.Fatalf("%s without manage_settings: %d", m, w.Code)
		}
	}
	for _, m := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
		if w := serveAdminConnection(h, models.AdminPermManageSettings, false, m, save); w.Code != http.StatusForbidden || responseCode(w) != "reauth_required" {
			t.Fatalf("%s without a fresh confirmation: %d %s", m, w.Code, w.Body.String())
		}
	}
	if conns.row != nil || svc.Enabled() {
		t.Fatal("a refused request changed the connection")
	}

	w := serveAdminConnection(h, models.AdminPermManageSettings, true, http.MethodPut, save)
	if w.Code != http.StatusOK || !svc.Enabled() || conns.row == nil {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	w = serveAdminConnection(h, models.AdminPermManageSettings, false, http.MethodGet, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "whsec_hidden_value") || !strings.Contains(w.Body.String(), `"webhook_secret_set":true`) {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), store.WebhookPath) {
		t.Fatalf("the view does not name the webhook URL: %s", w.Body.String())
	}
	if w := serveAdminConnection(h, models.AdminPermManageSettings, true, http.MethodDelete, ""); w.Code != http.StatusOK || svc.Enabled() {
		t.Fatalf("delete: %d, enabled %v", w.Code, svc.Enabled())
	}
}
