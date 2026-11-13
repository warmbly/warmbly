package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/advisor"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/leadsync"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/websitetracking"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// countingTools records every advisor tool call, so a refused fix provably ran nothing.
type countingTools struct{ calls atomic.Int32 }

func (t *countingTools) Call(context.Context, aitools.Invocation, string, json.RawMessage) (string, error) {
	t.calls.Add(1)
	return "ok", nil
}

// A member of two workspaces, acting in A, reaches none of B's integration connections, meetings, lead sync sources, advisor findings, email images or website tracking settings.
func TestLiveTenantFamiliesRefuseAnotherOrganization(t *testing.T) {
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
	contactA, contactB := uuid.New(), uuid.New()
	googleA, googleB, inboundB, zapierB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	subB, meetingB, sourceB, findingB, imageB := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgA, orgB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner)
	})
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Tenant', 'Families', $2)`, owner, owner.String()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'A', $3), ($2, 'B', $3)`, orgA, orgB, owner)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at)
	      VALUES ($1, $3, 'owner', -1, NOW()), ($2, $3, 'owner', -1, NOW())`, orgA, orgB, owner)
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
	      VALUES ($1, $3, $4, $5, 'A', 'Contact', '', '', '{}'), ($2, $3, $6, $7, 'B', 'Contact', '', '', '{}')`,
		contactA, contactB, owner, orgA, contactA.String()+"@example.test", orgB, contactB.String()+"@example.test")
	exec(`INSERT INTO integration_connections (id, organization_id, provider, label, status, inbound_secret) VALUES
	      ($1, $5, 'google_sheets', 'A sheets', 'connected', NULL),
	      ($2, $6, 'google_sheets', 'B sheets', 'connected', NULL),
	      ($3, $6, 'calendly', 'B calendly', 'connected', 'b-inbound-fixture'),
	      ($4, $6, 'zapier', 'B zapier', 'connected', NULL)`, googleA, googleB, inboundB, zapierB, orgA, orgB)
	exec(`INSERT INTO integration_event_subscriptions (id, connection_id, organization_id, event_type, action) VALUES ($1, $2, $3, 'reply.received', 'webhook')`, subB, zapierB, orgB)
	exec(`INSERT INTO integration_field_mappings (connection_id, organization_id, warmbly_field, external_field) VALUES ($1, $2, 'email', 'Email')`, zapierB, orgB)
	exec(`INSERT INTO integration_sync_runs (connection_id, organization_id, kind, status) VALUES ($1, $2, 'push', 'success')`, zapierB, orgB)
	exec(`INSERT INTO meeting_bookings (id, organization_id, source, external_event_id, invitee_email, event_name, scheduled_for, contact_id)
	      VALUES ($1, $2, 'manual', $5, $3, 'B call', NOW() + interval '1 day', $4)`, meetingB, orgB, contactB.String()+"@example.test", contactB, meetingB.String())
	exec(`INSERT INTO lead_sync_sources (id, organization_id, created_by_user_id, connection_id, sheet_id, label) VALUES ($1, $2, $3, $4, 'b-sheet', 'B source')`, sourceB, orgB, owner, googleB)
	exec(`INSERT INTO advisor_findings (id, organization_id, fingerprint, detector_key, category, severity, title, status, action)
	      VALUES ($1, $2, $3, 'fixture', 'mailbox', 'high', 'B finding', 'applied',
	              '{"tool":"update_mailbox","args":{},"label":"fix","undo":{"tool":"update_mailbox","args":{}}}')`, findingB, orgB, findingB.String())
	exec(`INSERT INTO email_images (id, organization_id, filename, storage_key, url) VALUES ($1, $2, 'b.png', 'public/email-images/b.png', 'https://example.test/b.png')`, imageB, orgB)

	tracking := websitetracking.NewService(repository.NewWebsiteTrackingRepository(pool), nil, nil)
	settingsB, xerr := tracking.UpdateSettings(ctx, orgB, owner, &models.UpdateWebsiteTrackingSettingsRequest{AllowedHosts: &[]string{"b.example.test"}})
	if xerr != nil {
		t.Fatal(xerr)
	}

	tools := &countingTools{}
	integrations := integration.NewService(repository.NewIntegrationRepository(pool), nil, nil)
	contacts := contact.NewService(repository.NewContactRepostory(handle), nil)
	h := &Handler{
		AuditService:           audit.NewNoOpService(),
		IntegrationService:     integrations,
		ContactRepo:            repository.NewContactRepostory(handle),
		ContactService:         contacts,
		LeadSyncService:        leadsync.NewService(repository.NewLeadSyncRepository(pool), integrations, contacts),
		AdvisorService:         advisor.NewService(repository.NewAdvisorRepository(handle), tools, nil, nil, nil, nil),
		EmailImageRepo:         repository.NewEmailImageRepository(handle),
		WebsiteTrackingService: tracking,
		OrganizationService:    organization.NewService(repository.NewOrganizationRepository(pool), nil, nil, nil, nil),
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
	ig.GET("/connections/:id", read, h.GetIntegrationConnection)
	ig.PATCH("/connections/:id/config", write, h.UpdateConnectionConfig)
	ig.DELETE("/connections/:id", write, h.DisconnectIntegration)
	ig.GET("/connections/:id/events", read, h.ListConnectionEventSubscriptions)
	ig.POST("/connections/:id/events", write, h.CreateConnectionEventSubscription)
	ig.DELETE("/connections/:id/events/:eventId", write, h.DeleteConnectionEventSubscription)
	ig.GET("/connections/:id/field-mappings", read, h.ListConnectionFieldMappings)
	ig.PUT("/connections/:id/field-mappings", write, h.ReplaceConnectionFieldMappings)
	ig.GET("/connections/:id/runs", read, h.ListConnectionSyncRuns)
	ig.GET("/connections/:id/webhook-secret", write, h.GetConnectionWebhookSecret)
	ig.PUT("/connections/:id/signing-key", write, h.SetConnectionSigningKey)
	ig.POST("/connections/:id/rotate-inbound-url", write, h.RotateConnectionInboundURL)
	ig.POST("/connections/:id/test", write, h.TestConnection)
	ig.POST("/connections/:id/push", operate, h.PushContactsToIntegration)
	meetingsRead := m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts)
	meetingsWrite := m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts)
	router.GET("/v1/meetings", meetingsRead, h.SearchMeetings)
	router.POST("/v1/meetings", meetingsWrite, h.CreateMeeting)
	router.DELETE("/v1/meetings/:id", meetingsWrite, h.DeleteMeeting)
	ls := router.Group("/v1/lead-sync", m.RequireAccess(models.PermManageContacts, models.APIPermWriteContacts))
	ls.GET("/sources", h.ListLeadSyncSources)
	ls.POST("/sources", h.CreateLeadSyncSource)
	ls.GET("/sources/:id", h.GetLeadSyncSource)
	ls.PATCH("/sources/:id", h.UpdateLeadSyncSource)
	ls.DELETE("/sources/:id", h.DeleteLeadSyncSource)
	ls.POST("/sources/:id/sync", h.SyncLeadSyncSourceNow)
	adv := router.Group("/v1/advisor", m.RequireAccess(models.PermViewAnalytics, models.APIPermReadAnalytics))
	adv.GET("/recommendations", h.ListAdvisorFindings)
	for _, verb := range []string{"apply", "undo", "snooze", "dismiss", "feedback"} {
		handler := map[string]gin.HandlerFunc{"apply": h.ApplyAdvisorFinding, "undo": h.UndoAdvisorFinding, "snooze": h.SnoozeAdvisorFinding, "dismiss": h.DismissAdvisorFinding, "feedback": h.SubmitAdvisorFeedback}[verb]
		adv.POST("/recommendations/:id/"+verb, handler)
	}
	router.GET("/v1/email-images", m.RequireAccess(models.PermViewCampaigns, models.APIPermReadCampaigns), h.ListEmailImages)
	router.DELETE("/v1/email-images/:id", m.RequireAccess(models.PermManageCampaigns, models.APIPermWriteCampaigns), h.DeleteEmailImage)
	router.GET("/v1/website-tracking/settings", m.RequirePermission(models.PermManageSettings), h.GetWebsiteTrackingSettings)
	router.PATCH("/v1/website-tracking/settings", m.RequirePermission(models.PermManageSettings), h.UpdateWebsiteTrackingSettings)
	router.POST("/v1/website-tracking/settings/rotate-key", m.RequirePermission(models.PermManageSettings), h.RotateWebsiteTrackingKey)

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
	// refused is any non-success answer; the row checks below prove nothing changed.
	refused := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code < 400 {
			t.Fatalf("status %d, want a refusal: %s", rec.Code, rec.Body.String())
		}
	}
	notFound := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body.String())
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
	conn := "/v1/integrations/connections/"

	// The same requests with B selected reach B's rows, so every refusal below is the tenant boundary and nothing else.
	t.Run("control: B reaches its own rows", func(t *testing.T) {
		selected = orgB
		defer func() { selected = orgA }()
		for _, c := range []struct {
			path string
			id   uuid.UUID
		}{
			{conn + zapierB.String() + "/events", subB},
			{"/v1/meetings?timeframe=upcoming", meetingB},
			{"/v1/lead-sync/sources/" + sourceB.String(), sourceB},
			{"/v1/advisor/recommendations", findingB},
			{"/v1/email-images", imageB},
		} {
			rec := call("GET", c.path, nil)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), c.id.String()) {
				t.Fatalf("GET %s as B: status %d: %s", c.path, rec.Code, rec.Body.String())
			}
		}
		if rec := call("GET", conn+zapierB.String(), nil); rec.Code != http.StatusOK {
			t.Fatalf("GET connection as B: status %d", rec.Code)
		}
	})

	t.Run("integration connections", func(t *testing.T) {
		t.Run("read connection", func(t *testing.T) { notFound(t, call("GET", conn+zapierB.String(), nil)) })
		t.Run("update config", func(t *testing.T) {
			refused(t, call("PATCH", conn+zapierB.String()+"/config", map[string]any{"sync_direction": "both"}))
		})
		t.Run("disconnect", func(t *testing.T) { call("DELETE", conn+zapierB.String(), nil) })
		t.Run("list event subscriptions", func(t *testing.T) { mentionsNone(t, call("GET", conn+zapierB.String()+"/events", nil), subB) })
		t.Run("create event subscription", func(t *testing.T) {
			refused(t, call("POST", conn+zapierB.String()+"/events", map[string]any{"event_type": "reply.received", "action": "webhook"}))
		})
		t.Run("delete event subscription", func(t *testing.T) { call("DELETE", conn+zapierB.String()+"/events/"+subB.String(), nil) })
		t.Run("list field mappings", func(t *testing.T) {
			rec := call("GET", conn+zapierB.String()+"/field-mappings", nil)
			mentionsNone(t, rec)
			if strings.Contains(rec.Body.String(), "Email") {
				t.Fatalf("response carries organization B's mapping: %s", rec.Body.String())
			}
		})
		t.Run("replace field mappings", func(t *testing.T) {
			refused(t, call("PUT", conn+zapierB.String()+"/field-mappings", map[string]any{"mappings": []map[string]string{{"warmbly_field": "first_name", "external_field": "Name"}}}))
		})
		t.Run("list sync runs", func(t *testing.T) {
			rec := call("GET", conn+zapierB.String()+"/runs", nil)
			mentionsNone(t, rec)
			if strings.Contains(rec.Body.String(), "success") {
				t.Fatalf("response carries organization B's run: %s", rec.Body.String())
			}
		})
		t.Run("read webhook signing secret", func(t *testing.T) {
			rec := call("GET", conn+zapierB.String()+"/webhook-secret", nil)
			refused(t, rec)
			if strings.Contains(rec.Body.String(), "signing_secret") {
				t.Fatalf("response carries a secret: %s", rec.Body.String())
			}
		})
		t.Run("set inbound signing key", func(t *testing.T) {
			notFound(t, call("PUT", conn+inboundB.String()+"/signing-key", map[string]string{"signing_key": strings.Repeat("k", 32)}))
		})
		t.Run("rotate inbound url", func(t *testing.T) { notFound(t, call("POST", conn+inboundB.String()+"/rotate-inbound-url", nil)) })
		t.Run("send test event", func(t *testing.T) { refused(t, call("POST", conn+zapierB.String()+"/test", nil)) })
		t.Run("push contacts", func(t *testing.T) {
			refused(t, call("POST", conn+zapierB.String()+"/push", map[string]any{"contact_ids": []string{contactA.String()}}))
		})
		t.Run("B's connections are untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM integration_connections WHERE organization_id = $1`, orgB); n != 3 {
				t.Fatalf("B has %d connections, want 3", n)
			}
			if d := one(`SELECT sync_direction FROM integration_connections WHERE id = $1`, zapierB); d != "push" {
				t.Fatalf("sync_direction = %q", d)
			}
			if s := one(`SELECT inbound_secret FROM integration_connections WHERE id = $1`, inboundB); s != "b-inbound-fixture" {
				t.Fatal("inbound secret changed")
			}
			if n := count(`SELECT count(*) FROM integration_connections WHERE id = $1 AND config_encrypted IS NOT NULL AND config_encrypted <> ''`, inboundB); n != 0 {
				t.Fatal("signing config written")
			}
			if n := count(`SELECT count(*) FROM integration_event_subscriptions WHERE organization_id = $1`, orgB); n != 1 {
				t.Fatalf("B has %d subscriptions, want 1", n)
			}
			if n := count(`SELECT count(*) FROM integration_field_mappings WHERE organization_id = $1 AND external_field = 'Email'`, orgB); n != 1 {
				t.Fatalf("B's mapping changed (%d)", n)
			}
			if n := count(`SELECT count(*) FROM integration_event_subscriptions WHERE organization_id = $1`, orgA); n != 0 {
				t.Fatal("a subscription was created on B's connection")
			}
		})
	})

	t.Run("meetings", func(t *testing.T) {
		t.Run("search", func(t *testing.T) { mentionsNone(t, call("GET", "/v1/meetings?timeframe=upcoming", nil), meetingB) })
		t.Run("delete", func(t *testing.T) { notFound(t, call("DELETE", "/v1/meetings/"+meetingB.String(), nil)) })
		t.Run("create linked to B's contact", func(t *testing.T) {
			rec := call("POST", "/v1/meetings", map[string]any{
				"invitee_email": contactB.String() + "@example.test", "scheduled_for": "2030-01-01T10:00:00Z", "contact_id": contactB.String(),
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if n := count(`SELECT count(*) FROM meeting_bookings WHERE organization_id = $1 AND contact_id IS NOT NULL`, orgA); n != 0 {
				t.Fatal("A's meeting was linked to B's contact")
			}
		})
		t.Run("B's meeting is untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM meeting_bookings WHERE id = $1 AND organization_id = $2`, meetingB, orgB); n != 1 {
				t.Fatal("B's meeting is gone")
			}
		})
	})

	t.Run("lead sync", func(t *testing.T) {
		src := "/v1/lead-sync/sources/" + sourceB.String()
		t.Run("list", func(t *testing.T) { mentionsNone(t, call("GET", "/v1/lead-sync/sources", nil), sourceB) })
		t.Run("read", func(t *testing.T) { notFound(t, call("GET", src, nil)) })
		t.Run("update", func(t *testing.T) { notFound(t, call("PATCH", src, map[string]any{"label": "A"})) })
		t.Run("delete", func(t *testing.T) { notFound(t, call("DELETE", src, nil)) })
		t.Run("sync now", func(t *testing.T) { notFound(t, call("POST", src+"/sync", nil)) })
		t.Run("create on B's Google connection", func(t *testing.T) {
			refused(t, call("POST", "/v1/lead-sync/sources", map[string]any{
				"connection_id": googleB, "sheet_id": "x", "dedup": "skip",
				"column_mapping": []map[string]any{{"index": 0, "target": "email"}},
			}))
		})
		t.Run("B's source is untouched", func(t *testing.T) {
			if l := one(`SELECT label FROM lead_sync_sources WHERE id = $1`, sourceB); l != "B source" {
				t.Fatalf("label = %q", l)
			}
			if n := count(`SELECT count(*) FROM lead_sync_sources WHERE organization_id = $1`, orgA); n != 0 {
				t.Fatal("a source was created on B's connection")
			}
		})
	})

	t.Run("advisor", func(t *testing.T) {
		rec := "/v1/advisor/recommendations/" + findingB.String()
		t.Run("list", func(t *testing.T) { mentionsNone(t, call("GET", "/v1/advisor/recommendations", nil), findingB) })
		t.Run("apply", func(t *testing.T) { notFound(t, call("POST", rec+"/apply", nil)) })
		t.Run("undo", func(t *testing.T) { notFound(t, call("POST", rec+"/undo", nil)) })
		t.Run("snooze", func(t *testing.T) { notFound(t, call("POST", rec+"/snooze", map[string]int{"days": 7})) })
		t.Run("dismiss", func(t *testing.T) { notFound(t, call("POST", rec+"/dismiss", map[string]string{"reason": "A"})) })
		t.Run("feedback", func(t *testing.T) { notFound(t, call("POST", rec+"/feedback", map[string]any{"helpful": false})) })
		t.Run("B's finding is untouched and no fix ran", func(t *testing.T) {
			if n := tools.calls.Load(); n != 0 {
				t.Fatalf("%d advisor tool calls ran", n)
			}
			if s := one(`SELECT status FROM advisor_findings WHERE id = $1`, findingB); s != "applied" {
				t.Fatalf("status = %q", s)
			}
			if n := count(`SELECT count(*) FROM advisor_findings WHERE id = $1 AND snoozed_until IS NULL AND dismissed_at IS NULL`, findingB); n != 1 {
				t.Fatal("B's finding was snoozed or dismissed")
			}
		})
	})

	t.Run("email images", func(t *testing.T) {
		t.Run("list", func(t *testing.T) { mentionsNone(t, call("GET", "/v1/email-images", nil), imageB) })
		t.Run("delete", func(t *testing.T) { notFound(t, call("DELETE", "/v1/email-images/"+imageB.String(), nil)) })
		t.Run("B's image is untouched", func(t *testing.T) {
			if n := count(`SELECT count(*) FROM email_images WHERE id = $1`, imageB); n != 1 {
				t.Fatal("B's image is gone")
			}
		})
	})

	t.Run("website tracking", func(t *testing.T) {
		t.Run("read", func(t *testing.T) {
			rec := call("GET", "/v1/website-tracking/settings", nil)
			if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), settingsB.SiteKey) || strings.Contains(rec.Body.String(), "b.example.test") {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
		t.Run("update", func(t *testing.T) {
			if rec := call("PATCH", "/v1/website-tracking/settings", map[string]any{"allowed_hosts": []string{"a.example.test"}}); rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
		t.Run("rotate key", func(t *testing.T) {
			if rec := call("POST", "/v1/website-tracking/settings/rotate-key", nil); rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
		t.Run("B's settings are untouched", func(t *testing.T) {
			after, xerr := tracking.GetSettings(ctx, orgB)
			if xerr != nil {
				t.Fatal(xerr)
			}
			if after.SiteKey != settingsB.SiteKey || len(after.AllowedHosts) != 1 || after.AllowedHosts[0] != "b.example.test" {
				t.Fatalf("B's settings changed: key rotated=%v hosts=%v", after.SiteKey != settingsB.SiteKey, after.AllowedHosts)
			}
		})
	})
}
