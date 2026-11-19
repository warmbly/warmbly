package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/partner"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// pausedPartnerOrgs reports one workspace as managed by a partner that suspended it.
type pausedPartnerOrgs struct {
	repository.PartnerRepository
	org uuid.UUID
}

func (pausedPartnerOrgs) HasPartnerOrganizations(context.Context) (bool, error) { return true, nil }

func (r pausedPartnerOrgs) GetPartnerOrgByOrganization(_ context.Context, orgID uuid.UUID) (*models.PartnerOrganization, string, error) {
	if orgID != r.org {
		return nil, "", nil
	}
	return &models.PartnerOrganization{OrganizationID: &orgID, State: models.PartnerOrgSuspended, Mode: models.PartnerModeLive}, "GraphKit", nil
}

// Over MCP a workspace its partner paused stays readable, and no tool that changes it runs.
func TestMCPRefusesWriteToolsInAPausedWorkspace(t *testing.T) {
	w := newMCPWorld(t, models.APIPermAgencyRead|models.APIPermReadCampaigns|models.APIPermWriteCampaigns)
	now := time.Now()
	w.members.members[w.client] = &models.OrganizationMember{OrganizationID: w.client, UserID: w.owner, Role: string(models.RoleAdmin),
		Permissions: models.PermViewCampaigns | models.PermManageCampaigns, AcceptedAt: &now}
	w.h.AITools.Register(aitools.Tool{
		Name: "rename_campaign", Description: "Renames a campaign.", Risk: generation.RiskWrite,
		RequiredAPIPerm: models.APIPermWriteCampaigns, RequiredOrgPerm: models.PermManageCampaigns,
		Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
			w.writes++
			return "renamed", nil
		},
	})
	call := func(name string, args map[string]any) rpcReply {
		args["organization_id"] = w.client.String()
		_, got := w.rpc(t, "", "tools/call", map[string]any{"name": name, "arguments": args})
		return got
	}
	if got := call("rename_campaign", map[string]any{}); got.Error != nil || w.writes != 1 {
		t.Fatalf("an active workspace refused a write: %+v", got.Error)
	}

	w.h.Partners = partner.NewService(partner.Deps{Repo: pausedPartnerOrgs{org: w.client}})
	got := call("rename_campaign", map[string]any{})
	if got.Error == nil || w.writes != 1 || !strings.Contains(got.Error.Message, "paused") {
		t.Fatalf("a paused workspace ran a write tool: %+v (writes=%d)", got.Error, w.writes)
	}
	got = call(mcpCallTool, map[string]any{"name": "rename_campaign", "arguments": map[string]any{"organization_id": w.client.String()}})
	if got.Error == nil || w.writes != 1 {
		t.Fatalf("call_tool reached a write tool in a paused workspace: %+v", got.Error)
	}
	if got := call("read_campaigns", map[string]any{"q": "acme"}); got.Error != nil || got.Result.IsError {
		t.Fatalf("a paused workspace refused a read: %+v %+v", got.Error, got.Result)
	}
}
