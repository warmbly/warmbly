package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Issue #531: exercise teammate requests through the real permission, handler, service and SQL boundaries.
func TestLiveSharedCampaignAccess(t *testing.T) {
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
	author, teammate, viewer := uuid.New(), uuid.New(), uuid.New()
	org, otherOrg, campaignID, otherCampaign, siblingCampaign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	step, foreignStep, siblingStep, contact := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{author, teammate, viewer} {
		exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Issue531', 'Test')`, user, user.String()+"@test.local")
	}
	t.Cleanup(func() {
		exec(`DELETE FROM campaigns WHERE organization_id = ANY($1)`, []uuid.UUID{org, otherOrg})
		exec(`DELETE FROM contacts WHERE organization_id = ANY($1)`, []uuid.UUID{org, otherOrg})
		exec(`DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{org, otherOrg})
		exec(`DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{author, teammate, viewer})
	})
	for _, id := range []uuid.UUID{org, otherOrg} {
		exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue531', $2, $3)`, id, id.String(), teammate)
		exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at) VALUES ($1, $2, 'owner', $5, NOW()), ($1, $3, 'admin', $5, NOW()), ($1, $4, 'viewer', $6, NOW())`, id, teammate, author, viewer, int64(models.PermViewCampaigns|models.PermManageCampaigns|models.PermViewAnalytics), int64(models.PermViewCampaigns))
	}
	for _, c := range []struct{ id, org uuid.UUID }{{campaignID, org}, {siblingCampaign, org}, {otherCampaign, otherOrg}} {
		exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, updated_at, created_at) VALUES ($1, $2, $3, 'Shared campaign', '', 62, NOW(), NOW())`, c.id, author, c.org)
	}
	for _, s := range []struct{ id, campaign, org uuid.UUID }{{step, campaignID, org}, {foreignStep, otherCampaign, otherOrg}, {siblingStep, siblingCampaign, org}} {
		exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, position) VALUES ($1, $2, $3, 'Intro', 'Hello', 'Original body', '<div></div>', 1)`, s.id, s.campaign, s.org)
	}
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, updated_at, created_at) VALUES ($1, $2, $3, $4, 'A', 'Lead', '', '', '{}', NOW(), NOW())`, contact, author, org, contact.String()+"@test.local")
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, opened_at) VALUES ($1, $2, $3, '2026-09-16T12:00:00Z', '2026-09-16T12:05:00Z')`, campaignID, contact, step)
	campaignRepo := repository.NewCampaignRepostory(handle)
	h := &Handler{
		CampaignService:  campaign.NewService(campaignRepo, nil, nil, repository.NewCampaignLogRepository(handle), nil, nil, nil, nil, nil),
		SequenceService:  sequence.NewService(repository.NewSequenceRepostory(handle)),
		AnalyticsService: analytics.NewService(repository.NewAnalyticsRepository(handle), nil, campaignRepo, nil, nil),
		AuditService:     audit.NewNoOpService(),
	}
	m := &middleware.Handler{OrganizationService: organization.NewService(repository.NewOrganizationRepository(handle.Pool), nil, nil, nil, nil)}
	type route struct {
		method, path string
		handler      gin.HandlerFunc
		orgPerm      models.OrganizationPermission
		apiPerm      uint64
	}
	routes := []route{
		{"GET", "/campaigns/:id", h.GetCampaign, models.PermViewCampaigns, models.APIPermReadCampaigns},
		{"PATCH", "/campaigns/:id", h.UpdateCampaign, models.PermManageCampaigns, models.APIPermWriteCampaigns},
		{"GET", "/campaigns/:id/logs", h.GetCampaignLogs, models.PermViewCampaigns, models.APIPermReadCampaigns},
		{"GET", "/campaigns/:id/steps", h.GetSequences, models.PermViewCampaigns, models.APIPermReadCampaigns},
		{"POST", "/campaigns/:id/steps", h.CreateSequence, models.PermManageCampaigns, models.APIPermWriteCampaigns},
		{"PATCH", "/campaigns/:id/steps/:sid", h.UpdateSequence, models.PermManageCampaigns, models.APIPermWriteCampaigns},
		{"DELETE", "/campaigns/:id/steps/:sid", h.DeleteSequence, models.PermManageCampaigns, models.APIPermWriteCampaigns},
		{"PATCH", "/campaigns/:id/step-layout", h.PatchSequenceLayout, models.PermManageCampaigns, models.APIPermWriteCampaigns},
		{"GET", "/analytics/campaigns/:id", h.GetCampaignAnalytics, models.PermViewAnalytics, models.APIPermReadAnalytics},
		{"GET", "/analytics/campaigns/:id/daily", h.GetCampaignDailyStats, models.PermViewAnalytics, models.APIPermReadAnalytics},
		{"GET", "/analytics/campaigns/:id/hourly", h.GetCampaignHourlyStats, models.PermViewAnalytics, models.APIPermReadAnalytics},
		{"GET", "/analytics/campaigns/compare", h.CompareCampaigns, models.PermViewAnalytics, models.APIPermReadAnalytics},
	}
	request := func(t *testing.T, user uuid.UUID, selectedOrg *uuid.UUID, apiPerms *uint64, method, path, body string, status int) *httptest.ResponseRecorder {
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
		for _, r := range routes {
			router.Handle(r.method, r.path, m.RequireAccess(r.orgPerm, r.apiPerm), r.handler)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, rec.Code, status, rec.Body.String())
		}
		return rec
	}
	campaignPath := "/campaigns/" + campaignID.String()
	analyticsPath := "/analytics/campaigns/" + campaignID.String()
	dates := "from=2026-09-16&to=2026-09-17"
	for _, caller := range []struct {
		name  string
		user  uuid.UUID
		perms *uint64
	}{
		{"author", author, nil}, {"teammate", teammate, nil}, {"api_key", teammate, ptrCampaignAccess(models.APIPermReadCampaigns | models.APIPermWriteCampaigns | models.APIPermReadAnalytics)},
	} {
		t.Run(caller.name, func(t *testing.T) {
			get := func(t *testing.T, path string) *httptest.ResponseRecorder {
				t.Helper()
				return request(t, caller.user, &org, caller.perms, "GET", path, "", 200)
			}
			t.Run("campaign", func(t *testing.T) { get(t, campaignPath) })
			t.Run("steps", func(t *testing.T) {
				rec := get(t, campaignPath+"/steps")
				var steps []models.Sequence
				if err := json.Unmarshal(rec.Body.Bytes(), &steps); err != nil {
					t.Fatal(err)
				}
				if len(steps) != 1 || steps[0].ID != step {
					t.Fatalf("steps = %s, want original step", rec.Body.String())
				}
			})
			t.Run("analytics", func(t *testing.T) {
				rec := get(t, analyticsPath)
				var result models.CampaignAnalytics
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Summary.EmailsSent != 1 || result.Summary.UniqueOpens != 1 {
					t.Fatalf("summary = %+v, want 1 send and 1 open", result.Summary)
				}
			})
			for _, path := range []string{analyticsPath + "/daily?" + dates, analyticsPath + "/hourly?date=2026-09-16"} {
				t.Run(path, func(t *testing.T) {
					rec := get(t, path)
					var result struct{ Data []struct{ Sent, Opens int } }
					if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					sent, opens := 0, 0
					for _, row := range result.Data {
						sent += row.Sent
						opens += row.Opens
					}
					if sent != 1 || opens != 1 {
						t.Fatalf("stats = %s, want 1 send and 1 open", rec.Body.String())
					}
				})
			}
			t.Run("compare", func(t *testing.T) {
				rec := get(t, "/analytics/campaigns/compare?ids="+campaignID.String()+"&"+dates)
				var result models.CampaignComparison
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Campaigns) != 1 || result.Campaigns[0].EmailsSent != 1 {
					t.Fatalf("comparison = %s", rec.Body.String())
				}
			})
			t.Run("logs", func(t *testing.T) { get(t, campaignPath+"/logs") })
			t.Run("settings", func(t *testing.T) {
				rec := request(t, caller.user, &org, caller.perms, "PATCH", campaignPath, `{"description":"Edited by teammate"}`, 200)
				var updated models.Campaign
				if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
					t.Fatal(err)
				}
				if updated.Description != "Edited by teammate" {
					t.Fatalf("description = %q", updated.Description)
				}
			})
			t.Run("partial_ramp", func(t *testing.T) {
				request(t, caller.user, &org, caller.perms, "PATCH", campaignPath, `{"ramp_start":1}`, 200)
			})
			t.Run("folders_only", func(t *testing.T) {
				request(t, caller.user, &org, caller.perms, "PATCH", campaignPath, `{"folders":[]}`, 200)
			})
			t.Run("edit_step", func(t *testing.T) {
				exec(`UPDATE sequences SET body_plain = 'Original body', body_html = '<div></div>' WHERE id = $1`, step)
				rec := request(t, caller.user, &org, caller.perms, "PATCH", campaignPath+"/steps/"+step.String(), `{"body_plain":"Edited body"}`, 200)
				var updated models.Sequence
				if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
					t.Fatal(err)
				}
				if updated.BodyPlain != "Edited body" || !strings.Contains(updated.BodyHTML, "Edited body") {
					t.Fatalf("step body was not saved and rendered: %+v", updated)
				}
			})
			t.Run("layout", func(t *testing.T) {
				exec(`UPDATE sequences SET x = 0, y = 0 WHERE id = $1`, step)
				request(t, caller.user, &org, caller.perms, "PATCH", campaignPath+"/step-layout", fmt.Sprintf(`{"positions":[{"id":%q,"x":123,"y":456}]}`, step), 200)
				var x, y float64
				if err := handle.QueryRow(ctx, `SELECT x, y FROM sequences WHERE id = $1`, step).Scan(&x, &y); err != nil {
					t.Fatal(err)
				}
				if x != 123 || y != 456 {
					t.Fatalf("layout = %v,%v, want 123,456", x, y)
				}
			})
			t.Run("create_delete_step", func(t *testing.T) {
				rec := request(t, caller.user, &org, caller.perms, "POST", campaignPath+"/steps", "", 200)
				var created models.Sequence
				if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
					t.Fatal(err)
				}
				var inheritedOrg uuid.UUID
				if err := handle.QueryRow(ctx, `SELECT organization_id FROM sequences WHERE id = $1`, created.ID).Scan(&inheritedOrg); err != nil {
					t.Fatal(err)
				}
				if inheritedOrg != org {
					t.Fatalf("step organization = %s, want %s", inheritedOrg, org)
				}
				request(t, caller.user, &org, caller.perms, "DELETE", campaignPath+"/steps/"+created.ID.String(), "", 200)
				var remaining int
				if err := handle.QueryRow(ctx, `SELECT COUNT(*) FROM sequences WHERE id = $1`, created.ID).Scan(&remaining); err != nil {
					t.Fatal(err)
				}
				if remaining != 0 {
					t.Fatal("deleted step still exists")
				}
			})
		})
	}
	t.Run("workspace_isolation", func(t *testing.T) {
		for _, path := range []string{campaignPath, campaignPath + "/logs", analyticsPath, analyticsPath + "/daily?" + dates, analyticsPath + "/hourly?date=2026-09-16", "/analytics/campaigns/compare?ids=" + campaignID.String() + "," + otherCampaign.String() + "&" + dates} {
			t.Run(path, func(t *testing.T) { request(t, author, &otherOrg, nil, "GET", path, "", 404) })
		}
		t.Run("steps", func(t *testing.T) {
			rec := request(t, author, &otherOrg, nil, "GET", campaignPath+"/steps", "", 200)
			if rec.Body.String() != "[]" {
				t.Fatalf("foreign steps exposed: %s", rec.Body.String())
			}
		})
		for _, mutation := range []struct{ method, path, body string }{
			{"PATCH", campaignPath, `{"description":"forbidden"}`},
			{"POST", campaignPath + "/steps", ""},
			{"PATCH", campaignPath + "/steps/" + step.String(), `{"subject":"forbidden"}`},
			{"DELETE", campaignPath + "/steps/" + step.String(), ""},
		} {
			t.Run(mutation.method+mutation.path, func(t *testing.T) {
				request(t, author, &otherOrg, nil, mutation.method, mutation.path, mutation.body, 404)
			})
		}
	})
	t.Run("step_must_belong_to_campaign", func(t *testing.T) {
		for _, id := range []uuid.UUID{foreignStep, siblingStep} {
			for _, method := range []string{"PATCH", "DELETE"} {
				t.Run(method+id.String(), func(t *testing.T) {
					request(t, author, &org, nil, method, campaignPath+"/steps/"+id.String(), `{"subject":"forbidden"}`, 404)
				})
			}
		}
	})
	t.Run("layout_isolation", func(t *testing.T) {
		exec(`UPDATE sequences SET x = 0, y = 0 WHERE id = ANY($1)`, []uuid.UUID{step, foreignStep, siblingStep})
		request(t, author, &otherOrg, nil, "PATCH", campaignPath+"/step-layout", fmt.Sprintf(`{"positions":[{"id":%q,"x":99,"y":99}]}`, step), 200)
		for _, id := range []uuid.UUID{foreignStep, siblingStep} {
			request(t, author, &org, nil, "PATCH", campaignPath+"/step-layout", fmt.Sprintf(`{"positions":[{"id":%q,"x":99,"y":99}]}`, id), 200)
		}
		var changed int
		if err := handle.QueryRow(ctx, `SELECT COUNT(*) FROM sequences WHERE id = ANY($1) AND (x != 0 OR y != 0)`, []uuid.UUID{step, foreignStep, siblingStep}).Scan(&changed); err != nil {
			t.Fatal(err)
		}
		if changed != 0 {
			t.Fatal("layout changed a step outside the selected workspace or campaign")
		}
	})
	t.Run("one_time_limit", func(t *testing.T) {
		exec(`UPDATE campaigns SET kind = 'one_time' WHERE id = $1`, campaignID)
		t.Cleanup(func() { exec(`UPDATE campaigns SET kind = 'sequence' WHERE id = $1`, campaignID) })
		request(t, teammate, &org, nil, "POST", campaignPath+"/steps", "", 400)
	})
	t.Run("creator_attribution", func(t *testing.T) {
		var creator uuid.UUID
		if err := handle.QueryRow(ctx, `SELECT user_id FROM campaigns WHERE id = $1`, campaignID).Scan(&creator); err != nil {
			t.Fatal(err)
		}
		if creator != author {
			t.Fatal("editing a shared campaign changed its creator")
		}
	})
	t.Run("permissions", func(t *testing.T) {
		request(t, viewer, &org, nil, "GET", campaignPath+"/steps", "", 200)
		request(t, viewer, &org, nil, "GET", analyticsPath, "", 403)
		for _, r := range routes {
			path := strings.ReplaceAll(strings.ReplaceAll(r.path, ":id", campaignID.String()), ":sid", step.String())
			t.Run(r.method+path, func(t *testing.T) {
				request(t, teammate, nil, ptrCampaignAccess(r.apiPerm), r.method, path, `{}`, 400)
				request(t, teammate, &org, ptrCampaignAccess(0), r.method, path, `{}`, 403)
				if r.method != "GET" {
					request(t, viewer, &org, nil, r.method, path, `{}`, 403)
				}
			})
		}
	})
}

func ptrCampaignAccess(v uint64) *uint64 { return &v }
