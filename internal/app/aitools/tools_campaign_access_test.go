package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type campaignAnalyticsAccessStub struct {
	analytics.AnalyticsService
	calls         int
	org, campaign uuid.UUID
}

func (s *campaignAnalyticsAccessStub) GetCampaignAnalytics(_ context.Context, orgID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error) {
	s.calls++
	s.org, s.campaign = orgID, campaignID
	return &models.CampaignAnalytics{CampaignID: campaignID, Summary: models.CampaignSummary{EmailsSent: 7}}, nil
}

// The assistant and MCP must require analytics permission, just like the HTTP endpoint.
func TestCampaignStatsToolAccess(t *testing.T) {
	org, user, campaignID := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name            string
		orgPerm         models.OrganizationPermission
		apiPerm         uint64
		apiKey, allowed bool
	}{
		{"member_without_analytics", models.PermViewCampaigns, models.APIPermReadAnalytics, false, false},
		{"member_with_analytics", models.PermViewAnalytics, 0, false, true},
		{"key_without_analytics", models.PermViewAnalytics, models.APIPermReadCampaigns, true, false},
		{"key_with_analytics", 0, models.APIPermReadAnalytics, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &campaignAnalyticsAccessStub{}
			registry := NewRegistry()
			Deps{Analytics: service}.registerCampaignTools(registry)
			inv := Invocation{OrgID: org, UserID: user, OrgPerms: tc.orgPerm, APIPerms: tc.apiPerm, IsAPIKey: tc.apiKey}
			listed := false
			for _, tool := range registry.PermittedTools(inv) {
				if tool.Name == "get_campaign_stats" {
					listed = true
				}
			}
			if listed != tc.allowed {
				t.Errorf("tool visible = %v, want %v", listed, tc.allowed)
			}
			result, err := registry.Call(context.Background(), inv, "get_campaign_stats", json.RawMessage(fmt.Sprintf(`{"campaign_id":%q}`, campaignID)))
			if !tc.allowed {
				if !errors.Is(err, ErrToolForbidden) {
					t.Errorf("error = %v, want forbidden", err)
				}
				if service.calls != 0 {
					t.Error("denied caller reached campaign analytics")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if service.calls != 1 || service.org != org || service.campaign != campaignID {
				t.Fatalf("analytics lookup = %+v, want selected workspace and campaign", service)
			}
			if !strings.Contains(result, `"emails_sent":7`) {
				t.Fatalf("stats = %s, want 7 sends", result)
			}
		})
	}
}
