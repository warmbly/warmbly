package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/apollo"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/clay"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/salesforce"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// A member of two workspaces, acting in A, reaches none of B's Salesforce, Apollo or Clay connections, import sources, activity, record links or enrichment runs.
func TestLiveCRMSyncIntegrationsRefuseAnotherOrganization(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handle.Pool.Close)
	pool := handle.Pool
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	one := func(query string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, query, args...).Scan(&v); err != nil {
			t.Fatalf("read: %v", err)
		}
		return v
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	owner, orgA, orgB := uuid.New(), uuid.New(), uuid.New()
	contactA, contactB, campaignB := uuid.New(), uuid.New(), uuid.New()
	sfA, sfB, apA, apB, clB := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sfSourceA, sfSourceB, sfActivityB, sfLinkB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	apSourceA, apSourceB, apRunB := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgA, orgB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner)
	})
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Tenant', 'CRM', $2)`, owner, owner.String()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'A', $3), ($2, 'B', $3)`, orgA, orgB, owner)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at)
	      VALUES ($1, $3, 'owner', -1, NOW()), ($2, $3, 'owner', -1, NOW())`, orgA, orgB, owner)
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
	      VALUES ($1, $3, $4, $5, 'A', 'Contact', '', '', '{}'), ($2, $3, $6, $7, 'B', 'Contact', '', '', '{}')`,
		contactA, contactB, owner, orgA, contactA.String()+"@example.test", orgB, contactB.String()+"@example.test")
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, updated_at, created_at)
	      VALUES ($1, $2, $3, 'B campaign', '', 62, NOW(), NOW())`, campaignB, owner, orgB)
	exec(`INSERT INTO integration_connections (id, organization_id, provider, label, status) VALUES
	      ($1, $6, 'salesforce', 'A salesforce', 'connected'),
	      ($2, $7, 'salesforce', 'B salesforce', 'connected'),
	      ($3, $6, 'apollo', 'A apollo', 'connected'),
	      ($4, $7, 'apollo', 'B apollo', 'connected'),
	      ($5, $7, 'clay', 'B clay', 'connected')`, sfA, sfB, apA, apB, clB, orgA, orgB)
	exec(`INSERT INTO salesforce_import_sources (id, organization_id, connection_id, name, source_kind, sobject, source_id) VALUES
	      ($1, $3, $4, 'A import', 'list_view', 'Lead', '00BA0000000AAAA'),
	      ($2, $5, $6, 'B import', 'list_view', 'Lead', '00BB0000000BBBB')`, sfSourceA, sfSourceB, orgA, sfA, orgB, sfB)
	exec(`INSERT INTO salesforce_activity_queue (id, organization_id, connection_id, contact_id, contact_email, kind, dedupe_key, status, occurred_at)
	      VALUES ($1, $2, $3, $4, $5, 'sent', 'b-fixture', 'failed', NOW())`, sfActivityB, orgB, sfB, contactB, contactB.String()+"@example.test")
	exec(`INSERT INTO salesforce_record_links (id, organization_id, connection_id, contact_id, sobject, record_id)
	      VALUES ($1, $2, $3, $4, 'Lead', '00QB0000000BBBB')`, sfLinkB, orgB, sfB, contactB)
	exec(`INSERT INTO apollo_import_sources (id, organization_id, connection_id, name, list_id) VALUES
	      ($1, $3, $4, 'A list', 'a-list'), ($2, $5, $6, 'B list', 'b-list')`, apSourceA, apSourceB, orgA, apA, orgB, apB)
	exec(`INSERT INTO apollo_enrichment_runs (id, organization_id, connection_id, contact_ids, status, total)
	      VALUES ($1, $2, $3, $4, 'done', 1)`, apRunB, orgB, apB, []uuid.UUID{contactB})

	integrations := integration.NewService(repository.NewIntegrationRepository(pool), nil, nil)
	contacts := contact.NewService(repository.NewContactRepostory(handle), nil)
	apolloRepo := repository.NewApolloRepository(pool)
	h := &Handler{
		AuditService:        audit.NewNoOpService(),
		IntegrationService:  integrations,
		ContactRepo:         repository.NewContactRepostory(handle),
		ContactService:      contacts,
		SalesforceService:   salesforce.NewService(salesforce.Deps{Repo: repository.NewSalesforceRepository(pool), Integrations: integrations, Contacts: contacts}),
		ApolloService:       apollo.NewService(apollo.Deps{Repo: apolloRepo, Integrations: integrations, Contacts: contacts}),
		ClayService:         clay.NewService(clay.Deps{Repo: repository.NewClayRepository(pool), Integrations: integrations, Contacts: contacts}),
		OrganizationService: organization.NewService(repository.NewOrganizationRepository(pool), nil, nil, nil, nil),
	}
	m := &middleware.Handler{OrganizationService: h.OrganizationService}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	selected := orgA
	router.Use(func(c *gin.Context) {
		c.Set(middleware.AuthTypeKey, middleware.AuthTypeJWT)
		c.Set(middleware.UserIDKey, owner.String())
		c.Set(middleware.OrganizationIDKey, selected)
	})
	read := m.RequireAnyAccess(models.APIPermIntegrations, models.PermManageSettings, models.PermUseIntegrations)
	write := m.RequireAccess(models.PermManageSettings, models.APIPermIntegrations)
	operate := m.RequireAccess(models.PermUseIntegrations, models.APIPermIntegrations)
	ig := router.Group("/v1/integrations")
	sf := ig.Group("/salesforce/:id")
	sf.GET("/overview", read, h.SalesforceOverview)
	sf.GET("/settings", read, h.GetSalesforceSettings)
	sf.PUT("/settings", write, h.UpdateSalesforceSettings)
	sf.GET("/metadata", read, h.SalesforceMetadata)
	sf.GET("/users", read, h.SalesforceUsers)
	sf.GET("/list-views", read, h.SalesforceListViews)
	sf.GET("/campaigns", read, h.SalesforceCampaigns)
	sf.POST("/import/preview", operate, h.PreviewSalesforceImport)
	sf.GET("/import-sources", read, h.ListSalesforceImportSources)
	sf.POST("/import-sources", write, h.CreateSalesforceImportSource)
	sf.PATCH("/import-sources/:sourceId", write, h.UpdateSalesforceImportSource)
	sf.POST("/import-sources/:sourceId/run", operate, h.RunSalesforceImportSource)
	sf.DELETE("/import-sources/:sourceId", write, h.DeleteSalesforceImportSource)
	sf.GET("/activity", read, h.ListSalesforceActivity)
	sf.POST("/activity/retry", write, h.RetrySalesforceActivity)
	sf.POST("/sync-now", operate, h.SalesforceSyncNow)
	ap := ig.Group("/apollo/:id")
	ap.GET("", read, h.ApolloOverview)
	ap.PUT("/key", write, h.ReplaceApolloKey)
	ap.GET("/lists", read, h.ApolloLists)
	ap.POST("/import/preview", operate, h.PreviewApolloImport)
	ap.GET("/import-sources", read, h.ListApolloImportSources)
	ap.POST("/import-sources", write, h.CreateApolloImportSource)
	ap.PATCH("/import-sources/:sourceId", write, h.UpdateApolloImportSource)
	ap.POST("/import-sources/:sourceId/run", operate, h.RunApolloImportSource)
	ap.DELETE("/import-sources/:sourceId", write, h.DeleteApolloImportSource)
	ap.GET("/enrichments", read, h.ListApolloEnrichments)
	ap.POST("/enrichments", operate, m.RequireAccess(models.PermManageContacts, models.APIPermBulkContacts), h.StartApolloEnrichment)
	ap.GET("/enrichments/:runId", read, h.GetApolloEnrichment)
	cl := ig.Group("/clay/:id")
	cl.GET("", read, h.GetClaySettings)
	cl.PATCH("", write, h.UpdateClaySettings)
	ct := router.Group("/v1/contacts")
	ct.GET("/:id/salesforce", m.RequireAccess(models.PermViewContacts, models.APIPermReadCRM), h.GetContactSalesforce)
	ct.POST("/:id/salesforce/sync", m.RequireAccess(models.PermUseIntegrations, models.APIPermIntegrations), h.SyncContactSalesforce)
	ct.DELETE("/:id/salesforce/links/:linkId", m.RequireAccess(models.PermUseIntegrations, models.APIPermIntegrations), h.UnlinkContactSalesforce)

	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	notFound := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body.String())
		}
	}
	refused := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code < 400 {
			t.Fatalf("status %d, want a refusal: %s", rec.Code, rec.Body.String())
		}
	}
	mentionsNone := func(t *testing.T, rec *httptest.ResponseRecorder, ids ...uuid.UUID) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		for _, id := range ids {
			if strings.Contains(rec.Body.String(), id.String()) {
				t.Fatalf("response names organization B's row %s: %s", id, rec.Body.String())
			}
		}
	}
	sfBase := "/v1/integrations/salesforce/" + sfB.String()
	apBase := "/v1/integrations/apollo/" + apB.String()
	sfSourceBody := map[string]any{"source_kind": "list_view", "object": "Lead", "source_id": "00BA0000000CCCC"}

	// The same reads with B selected reach B's rows, so every refusal below is the tenant boundary and nothing else.
	t.Run("control: B reaches its own rows", func(t *testing.T) {
		selected = orgB
		defer func() { selected = orgA }()
		for _, c := range []struct {
			path string
			id   uuid.UUID
		}{
			{sfBase + "/import-sources", sfSourceB},
			{sfBase + "/activity", sfActivityB},
			{apBase, apB},
			{apBase + "/import-sources", apSourceB},
			{apBase + "/enrichments/" + apRunB.String(), apRunB},
		} {
			rec := call("GET", c.path, nil)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), c.id.String()) {
				t.Fatalf("GET %s as B: status %d: %s", c.path, rec.Code, rec.Body.String())
			}
		}
		if rec := call("GET", sfBase+"/settings", nil); rec.Code != http.StatusOK {
			t.Fatalf("GET salesforce settings as B: status %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("salesforce", func(t *testing.T) {
		for _, c := range []struct {
			name, method, path string
			body               any
		}{
			{"overview", "GET", "/overview", nil},
			{"read settings", "GET", "/settings", nil},
			{"save settings", "PUT", "/settings", salesforce.DefaultSettings()},
			{"metadata", "GET", "/metadata", nil},
			{"users", "GET", "/users?q=a", nil},
			{"list views", "GET", "/list-views?object=Lead", nil},
			{"campaigns", "GET", "/campaigns?q=a", nil},
			{"import preview", "POST", "/import/preview", sfSourceBody},
			{"create import source", "POST", "/import-sources", sfSourceBody},
			{"update import source", "PATCH", "/import-sources/" + sfSourceB.String(), map[string]any{"name": "A"}},
			{"run import source", "POST", "/import-sources/" + sfSourceB.String() + "/run", nil},
			{"delete import source", "DELETE", "/import-sources/" + sfSourceB.String(), nil},
			{"sync now", "POST", "/sync-now", nil},
		} {
			t.Run(c.name, func(t *testing.T) { notFound(t, call(c.method, sfBase+c.path, c.body)) })
		}
		own := "/v1/integrations/salesforce/" + sfA.String() + "/import-sources/"
		t.Run("B's source through A's connection", func(t *testing.T) {
			notFound(t, call("PATCH", own+sfSourceB.String(), map[string]any{"name": "A"}))
			notFound(t, call("POST", own+sfSourceB.String()+"/run", nil))
			notFound(t, call("DELETE", own+sfSourceB.String(), nil))
		})
		t.Run("enroll into B's campaign", func(t *testing.T) {
			refused(t, call("PATCH", own+sfSourceA.String(), map[string]any{"campaign_id": campaignB}))
			refused(t, call("POST", "/v1/integrations/salesforce/"+sfA.String()+"/import-sources", map[string]any{
				"source_kind": "list_view", "object": "Lead", "source_id": "00BA0000000DDDD", "campaign_id": campaignB,
			}))
		})
		t.Run("list import sources", func(t *testing.T) { mentionsNone(t, call("GET", sfBase+"/import-sources", nil), sfSourceB) })
		t.Run("list activity", func(t *testing.T) {
			mentionsNone(t, call("GET", sfBase+"/activity", nil), sfActivityB)
			mentionsNone(t, call("GET", "/v1/integrations/salesforce/"+sfA.String()+"/activity?contact_id="+contactB.String(), nil), sfActivityB)
		})
		t.Run("retry B's activity", func(t *testing.T) {
			for _, base := range []string{sfBase, "/v1/integrations/salesforce/" + sfA.String()} {
				rec := call("POST", base+"/activity/retry", map[string]any{"ids": []string{sfActivityB.String()}})
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"requeued":0`) {
					t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
			}
		})
		t.Run("B's rows are untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM salesforce_import_sources WHERE id = $1 AND name = 'B import' AND status = 'idle'`, sfSourceB); n != 1 {
				t.Fatal("B's import source changed or is gone")
			}
			if n := count(`SELECT count(*) FROM salesforce_import_sources WHERE organization_id = $1`, orgA); n != 1 {
				t.Fatalf("A has %d import sources, want 1", n)
			}
			if n := count(`SELECT count(*) FROM salesforce_import_sources WHERE campaign_id = $1 AND organization_id <> $2`, campaignB, orgB); n != 0 {
				t.Fatal("an import source enrolls into B's campaign")
			}
			if s := one(`SELECT status FROM salesforce_activity_queue WHERE id = $1`, sfActivityB); s != "failed" {
				t.Fatalf("B's activity status = %q", s)
			}
			if n := count(`SELECT count(*) FROM integration_connections WHERE id = $1 AND config_capabilities::text = '{}'`, sfB); n != 1 {
				t.Fatal("B's Salesforce settings were written")
			}
		})
	})

	t.Run("salesforce contact panel", func(t *testing.T) {
		panel := "/v1/contacts/" + contactB.String() + "/salesforce"
		t.Run("read B's contact", func(t *testing.T) { notFound(t, call("GET", panel, nil)) })
		t.Run("sync B's contact", func(t *testing.T) { notFound(t, call("POST", panel+"/sync", map[string]any{"connection_id": sfB})) })
		t.Run("sync own contact through B's connection", func(t *testing.T) {
			refused(t, call("POST", "/v1/contacts/"+contactA.String()+"/salesforce/sync", map[string]any{"connection_id": sfB}))
		})
		t.Run("unlink B's link", func(t *testing.T) {
			notFound(t, call("DELETE", panel+"/links/"+sfLinkB.String(), nil))
			notFound(t, call("DELETE", "/v1/contacts/"+contactA.String()+"/salesforce/links/"+sfLinkB.String(), nil))
		})
		t.Run("B's link is untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM salesforce_record_links WHERE id = $1`, sfLinkB); n != 1 {
				t.Fatal("B's record link is gone")
			}
		})
	})

	t.Run("apollo", func(t *testing.T) {
		for _, c := range []struct {
			name, method, path string
			body               any
		}{
			{"overview", "GET", "", nil},
			{"replace key", "PUT", "/key", map[string]string{"api_key": "fixture-not-a-key"}},
			{"lists", "GET", "/lists", nil},
			{"import preview", "POST", "/import/preview", map[string]string{"list_id": "b-list"}},
			{"create import source", "POST", "/import-sources", map[string]string{"list_id": "b-list"}},
			{"update import source", "PATCH", "/import-sources/" + apSourceB.String(), map[string]any{"name": "A"}},
			{"run import source", "POST", "/import-sources/" + apSourceB.String() + "/run", nil},
			{"delete import source", "DELETE", "/import-sources/" + apSourceB.String(), nil},
			{"read enrichment run", "GET", "/enrichments/" + apRunB.String(), nil},
			{"start enrichment", "POST", "/enrichments", map[string]any{"contact_ids": []string{contactA.String()}}},
		} {
			t.Run(c.name, func(t *testing.T) { notFound(t, call(c.method, apBase+c.path, c.body)) })
		}
		own := "/v1/integrations/apollo/" + apA.String()
		t.Run("B's source and run through A's connection", func(t *testing.T) {
			notFound(t, call("PATCH", own+"/import-sources/"+apSourceB.String(), map[string]any{"name": "A"}))
			notFound(t, call("POST", own+"/import-sources/"+apSourceB.String()+"/run", nil))
			notFound(t, call("DELETE", own+"/import-sources/"+apSourceB.String(), nil))
			notFound(t, call("GET", own+"/enrichments/"+apRunB.String(), nil))
		})
		t.Run("enroll into B's campaign", func(t *testing.T) {
			refused(t, call("PATCH", own+"/import-sources/"+apSourceA.String(), map[string]any{"campaign_id": campaignB}))
		})
		t.Run("list import sources", func(t *testing.T) { mentionsNone(t, call("GET", apBase+"/import-sources", nil), apSourceB) })
		t.Run("list enrichment runs", func(t *testing.T) { mentionsNone(t, call("GET", apBase+"/enrichments", nil), apRunB) })
		t.Run("enrichment reads only its own workspace's contacts", func(t *testing.T) {
			refs, err := apolloRepo.ContactsForEnrichment(ctx, orgA, []uuid.UUID{contactB})
			if err != nil || len(refs) != 0 {
				t.Fatalf("refs = %v, err = %v", refs, err)
			}
		})
		t.Run("B's rows are untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM apollo_import_sources WHERE id = $1 AND name = 'B list' AND status = 'idle'`, apSourceB); n != 1 {
				t.Fatal("B's import source changed or is gone")
			}
			if n := count(`SELECT count(*) FROM apollo_import_sources WHERE organization_id = $1 AND campaign_id IS NOT NULL`, orgA); n != 0 {
				t.Fatal("A's import source enrolls into B's campaign")
			}
			if n := count(`SELECT count(*) FROM apollo_enrichment_runs WHERE connection_id = $1`, apB); n != 1 {
				t.Fatal("an enrichment run was queued on B's connection")
			}
			if n := count(`SELECT count(*) FROM integration_connections WHERE id = $1 AND COALESCE(config_encrypted, '') = ''`, apB); n != 1 {
				t.Fatal("B's Apollo key was written")
			}
		})
	})

	t.Run("clay", func(t *testing.T) {
		base := "/v1/integrations/clay/" + clB.String()
		t.Run("read settings", func(t *testing.T) { notFound(t, call("GET", base, nil)) })
		t.Run("update settings", func(t *testing.T) {
			notFound(t, call("PATCH", base, map[string]any{"webhook_url": "https://api.clay.com/v3/sources/webhook/fixture"}))
		})
		t.Run("B's connection is untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM integration_connections WHERE id = $1 AND COALESCE(config_encrypted, '') = '' AND config_capabilities::text = '{}'`, clB); n != 1 {
				t.Fatal("B's Clay settings were written")
			}
		})
	})
}
