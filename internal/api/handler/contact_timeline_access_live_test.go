package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Issue #550: exercise organization-wide contact timeline access through the route boundary.
func TestLiveContactTimelineAccess(t *testing.T) {
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
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}

	author, teammate, viewer, restricted := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	org, otherOrg, contactID := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		for _, step := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM contact_notes WHERE contact_id = $1`, []any{contactID}},
			{`DELETE FROM contacts WHERE id = $1`, []any{contactID}},
			{`DELETE FROM organization_members WHERE organization_id = ANY($1)`, []any{[]uuid.UUID{org, otherOrg}}},
			{`DELETE FROM organizations WHERE id = ANY($1)`, []any{[]uuid.UUID{org, otherOrg}}},
			{`DELETE FROM users WHERE id = ANY($1)`, []any{[]uuid.UUID{author, teammate, viewer, restricted}}},
		} {
			if _, err := handle.Exec(cleanupCtx, step.query, step.args...); err != nil {
				t.Errorf("cleanup %q: %v", step.query, err)
			}
		}
	})
	for _, user := range []uuid.UUID{author, teammate, viewer, restricted} {
		exec(`INSERT INTO users (id, email, first_name, last_name, password_hash) VALUES ($1, $2, 'Issue550', 'Test', 'x')`, user, user.String()+"@test.local")
	}
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue 550', $2, $3), ($4, 'Other workspace', $5, $3)`, org, org.String(), author, otherOrg, otherOrg.String())
	exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at) VALUES
		($1, $2, 'owner', $7, NOW()),
		($1, $3, 'manager', $8, NOW()),
		($1, $4, 'viewer', $8, NOW()),
		($1, $5, 'manager', 0, NOW()),
		($6, $2, 'owner', $7, NOW())`,
		org, author, teammate, viewer, restricted, otherOrg, int64(-1), int64(models.PermViewContacts))
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, updated_at, created_at)
		VALUES ($1, $2, $3, $4, 'Timeline', 'Contact', '', '', '{}', NOW(), NOW())`, contactID, author, org, contactID.String()+"@test.local")
	exec(`INSERT INTO contact_notes (contact_id, organization_id, user_id, content) VALUES ($1, $2, $3, 'Visible to the workspace')`, contactID, org, author)

	h := &Handler{ContactService: contact.NewService(repository.NewContactRepostory(handle), nil)}
	m := &middleware.Handler{OrganizationService: organization.NewService(repository.NewOrganizationRepository(handle.Pool), nil, nil, nil, nil)}
	request := func(t *testing.T, user uuid.UUID, selectedOrg *uuid.UUID, apiPerms *uint64, status int) *httptest.ResponseRecorder {
		t.Helper()
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(middleware.UserIDKey, user.String())
			if selectedOrg != nil {
				c.Set(middleware.OrganizationIDKey, *selectedOrg)
			}
			if apiPerms != nil {
				c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
				c.Set(middleware.APIKeyPermissionsKey, *apiPerms)
			}
		})
		router.GET("/v1/contacts/:id/timeline", m.RequireAccess(models.PermViewContacts, models.APIPermReadContacts), h.ListContactTimeline)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/contacts/"+contactID.String()+"/timeline", nil))
		if rec.Code != status {
			t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
		}
		return rec
	}

	readContacts := uint64(models.APIPermReadContacts)
	for _, caller := range []struct {
		name  string
		user  uuid.UUID
		perms *uint64
	}{
		{"author", author, nil},
		{"teammate", teammate, nil},
		{"viewer", viewer, nil},
		{"api_key", teammate, &readContacts},
	} {
		t.Run(caller.name, func(t *testing.T) {
			rec := request(t, caller.user, &org, caller.perms, 200)
			var result models.ContactTimelineResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Data) != 1 || result.Data[0].Type != models.TimelineNote {
				t.Fatalf("timeline = %s, want the workspace note", rec.Body.String())
			}
		})
	}

	t.Run("workspace_isolation", func(t *testing.T) { request(t, author, &otherOrg, nil, 404) })
	t.Run("organization_required", func(t *testing.T) { request(t, author, nil, nil, 400) })
	t.Run("organization_permission_required", func(t *testing.T) { request(t, restricted, &org, nil, 403) })
	t.Run("api_permission_required", func(t *testing.T) {
		noPermissions := uint64(0)
		request(t, teammate, &org, &noPermissions, 403)
	})
}
