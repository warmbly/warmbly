package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type mcpAgencyKeys struct {
	repository.AgencyAPIKeyRepository
	byHash map[string]*models.AgencyAPIKey
}

func (r *mcpAgencyKeys) Create(_ context.Context, agencyID, createdBy uuid.UUID, d *models.CreateAgencyAPIKey, _, _, hash string) (*models.AgencyAPIKey, error) {
	k := &models.AgencyAPIKey{ID: uuid.New(), AgencyID: agencyID, CreatedBy: createdBy, Name: d.Name, Permissions: d.Permissions, Status: models.APIKeyStatusActive}
	r.byHash[hash] = k
	return k, nil
}

func (r *mcpAgencyKeys) GetActiveByHash(_ context.Context, hash string) (*models.AgencyAPIKey, error) {
	return r.byHash[hash], nil
}

func (r *mcpAgencyKeys) TouchLastUsed(context.Context, uuid.UUID, string) error { return nil }

type mcpAgencyStaff struct {
	agency  uuid.UUID
	owner   uuid.UUID
	clients map[uuid.UUID]uuid.UUID
}

func (s *mcpAgencyStaff) GetMember(_ context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error) {
	if agencyID != s.agency || userID != s.owner {
		return nil, nil
	}
	return &models.AgencyMember{AgencyID: agencyID, UserID: userID, Role: models.AgencyRoleOwner}, nil
}

func (s *mcpAgencyStaff) GetClient(_ context.Context, orgID uuid.UUID) (*models.AgencyClient, error) {
	a, ok := s.clients[orgID]
	if !ok {
		return nil, nil
	}
	return &models.AgencyClient{OrganizationID: orgID, AgencyID: a}, nil
}

type mcpMembers struct {
	organization.OrganizationService
	members map[uuid.UUID]*models.OrganizationMember
}

func (f *mcpMembers) GetMembership(_ context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, *errx.Error) {
	m := f.members[orgID]
	if m == nil || m.UserID != userID {
		return nil, nil
	}
	return m, nil
}

type mcpAgencyClients struct {
	aitools.AgencyClients
	calls int
}

func (f *mcpAgencyClients) Clients(_ context.Context, _, _ uuid.UUID) ([]models.AgencyClient, *errx.Error) {
	f.calls++
	return []models.AgencyClient{}, nil
}

// mcpWorld is one agency with one client, an agency key, and a workspace tool that reports how it was invoked.
type mcpWorld struct {
	r       *gin.Engine
	staff   *mcpAgencyStaff
	members *mcpMembers
	secret  string
	owner   uuid.UUID
	client  uuid.UUID
	agency  *mcpAgencyClients
	seen    *aitools.Invocation
}

func newMCPWorld(t *testing.T, keyPerms uint64) *mcpWorld {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := &mcpWorld{owner: uuid.New(), client: uuid.New(), agency: &mcpAgencyClients{}}
	w.staff = &mcpAgencyStaff{agency: uuid.New(), owner: w.owner, clients: map[uuid.UUID]uuid.UUID{}}
	w.staff.clients[w.client] = w.staff.agency
	keys := apikey.NewAgencyKeyService(&mcpAgencyKeys{byHash: map[string]*models.AgencyAPIKey{}}, w.staff, nil)
	k, xerr := keys.Create(context.Background(), w.staff.agency, w.owner, &models.CreateAgencyAPIKey{Name: "agent", Permissions: keyPerms})
	if xerr != nil {
		t.Fatal(xerr)
	}
	w.secret = k.Secret
	now := time.Now()
	w.members = &mcpMembers{members: map[uuid.UUID]*models.OrganizationMember{
		w.client: {OrganizationID: w.client, UserID: w.owner, Role: string(models.RoleViewer), Permissions: models.PermViewCampaigns, AcceptedAt: &now},
	}}

	reg := aitools.NewRegistry()
	reg.Register(aitools.Tool{
		Name: "read_campaigns", Description: "Reads campaigns.",
		InputSchema:     map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}, "required": []string{"q"}},
		Risk:            generation.RiskRead,
		RequiredAPIPerm: models.APIPermReadCampaigns, RequiredOrgPerm: models.PermViewCampaigns,
		Handler: func(_ context.Context, inv aitools.Invocation, args json.RawMessage) (string, error) {
			w.seen = &inv
			return string(args), nil
		},
	})
	reg.Register(aitools.Tool{
		Name: "write_campaigns", Description: "Writes campaigns.", Risk: generation.RiskWrite,
		RequiredAPIPerm: models.APIPermWriteCampaigns, RequiredOrgPerm: models.PermManageCampaigns,
		Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
			t.Error("write_campaigns ran for a member without the permission")
			return "", nil
		},
	})
	reg.Register(aitools.Tool{
		Name: "send_mail", Description: "Sends.", Risk: generation.RiskSend, RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) { return "", nil },
	})

	mw := &middleware.Handler{AgencyKeys: keys, OrganizationService: w.members}
	h := &Handler{AITools: reg, AgencyTools: aitools.BuildAgencyRegistry(aitools.AgencyDeps{Agency: w.agency})}
	w.r = gin.New()
	w.r.Use(middleware.RequestIDMiddleware())
	w.r.POST("/v1/mcp", mw.MCPAuthMiddleware(), mw.ResourceScopeGate(), h.MCPEndpoint)
	return w
}

type rpcReply struct {
	Result struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (w *mcpWorld) rpc(t *testing.T, org, method string, params any) (int, rpcReply) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	// ?tools=all lists every tool rather than the core set, so the assertions see each one.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/mcp?tools=all", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+w.secret)
	req.Header.Set("Content-Type", "application/json")
	if org != "" {
		req.Header.Set(models.AgencyOrganizationHeader, org)
	}
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	var out rpcReply
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func toolNames(r rpcReply) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, t := range r.Result.Tools {
		out[t.Name] = t.InputSchema
	}
	return out
}

func TestAgencyMCPListsAgencyToolsAndClientScopedWorkspaceTools(t *testing.T) {
	w := newMCPWorld(t, models.APIPermAgencyRead|models.APIPermReadCampaigns)
	code, got := w.rpc(t, "", "tools/list", nil)
	if code != http.StatusOK || got.Error != nil {
		t.Fatalf("tools/list = %d %+v", code, got.Error)
	}
	tools := toolNames(got)
	if _, ok := tools["list_clients"]; !ok {
		t.Errorf("agency tool missing: %v", tools)
	}
	if _, ok := tools["get_agency_usage"]; ok {
		t.Error("get_agency_usage listed without AGENCY_READ_USAGE")
	}
	if _, ok := tools["send_mail"]; ok {
		t.Error("a send-class tool was listed")
	}
	if _, ok := tools["write_campaigns"]; ok {
		t.Error("a tool beyond the key's scopes was listed")
	}
	schema, ok := tools["read_campaigns"]
	if !ok {
		t.Fatalf("workspace tool missing: %v", tools)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["organization_id"]; !ok {
		t.Errorf("organization_id not injected: %v", schema)
	}
	required, _ := schema["required"].([]any)
	if len(required) != 2 || required[0] != "organization_id" || required[1] != "q" {
		t.Errorf("required = %v, want organization_id and the tool's own", required)
	}
}

func TestAgencyMCPCallsRunInTheClientWithTheMembersPermissions(t *testing.T) {
	w := newMCPWorld(t, models.APIPermAgencyRead|models.APIPermReadCampaigns|models.APIPermWriteCampaigns)

	_, got := w.rpc(t, "", "tools/call", map[string]any{"name": "read_campaigns", "arguments": map[string]any{"organization_id": w.client.String(), "q": "acme"}})
	if got.Error != nil || got.Result.IsError || w.seen == nil {
		t.Fatalf("client call = %+v %+v", got.Error, got.Result)
	}
	if w.seen.OrgID != w.client || w.seen.UserID != w.owner || !w.seen.ActsForMember || w.seen.OrgPerms != models.PermViewCampaigns {
		t.Errorf("invocation = %+v", *w.seen)
	}
	if w.seen.APIPerms&^models.AgencyKeyWorkspaceScopes != 0 {
		t.Errorf("workspace call carried bits outside the agency workspace scopes: %b", w.seen.APIPerms)
	}
	if got.Result.Content[0].Text != `{"q":"acme"}` {
		t.Errorf("tool saw %s, want organization_id stripped", got.Result.Content[0].Text)
	}

	// The key holds the bit, the member does not: the member's role bounds the call.
	if _, got := w.rpc(t, "", "tools/call", map[string]any{"name": "write_campaigns", "arguments": map[string]any{"organization_id": w.client.String()}}); got.Error == nil {
		t.Error("write_campaigns ran past the member's role")
	}

	other := uuid.New()
	w.staff.clients[other] = uuid.New()
	for name, args := range map[string]map[string]any{
		"missing organization_id":  {"q": "acme"},
		"not a client":             {"organization_id": uuid.NewString(), "q": "acme"},
		"another agency's client":  {"organization_id": other.String(), "q": "acme"},
		"malformed organization":   {"organization_id": "nope", "q": "acme"},
		"organization_id not text": {"organization_id": 7, "q": "acme"},
	} {
		w.seen = nil
		if _, got := w.rpc(t, "", "tools/call", map[string]any{"name": "read_campaigns", "arguments": args}); got.Error == nil || w.seen != nil {
			t.Errorf("%s: call was not refused: %+v", name, got.Result)
		}
	}

	// A client the agency let go is refused on the very next call.
	delete(w.staff.clients, w.client)
	w.seen = nil
	if _, got := w.rpc(t, "", "tools/call", map[string]any{"name": "read_campaigns", "arguments": map[string]any{"organization_id": w.client.String(), "q": "acme"}}); got.Error == nil || w.seen != nil {
		t.Error("detached client still reachable")
	}
	w.staff.clients[w.client] = w.staff.agency

	// A restricted membership reaches nothing through MCP, as on the header path.
	w.members.members[w.client].AccessScope = models.AccessScopeRestricted
	w.seen = nil
	if _, got := w.rpc(t, "", "tools/call", map[string]any{"name": "read_campaigns", "arguments": map[string]any{"organization_id": w.client.String(), "q": "acme"}}); got.Error == nil || w.seen != nil {
		t.Error("restricted member reached a workspace tool")
	}
}

func TestAgencyMCPAgencyToolsNeedTheirBit(t *testing.T) {
	w := newMCPWorld(t, models.APIPermReadCampaigns)
	if _, got := w.rpc(t, "", "tools/call", map[string]any{"name": "list_clients", "arguments": map[string]any{}}); got.Error == nil || w.agency.calls != 0 {
		t.Errorf("list_clients ran without AGENCY_READ: %+v", got)
	}
	w = newMCPWorld(t, models.APIPermAgencyRead)
	if _, got := w.rpc(t, "", "tools/call", map[string]any{"name": "list_clients", "arguments": map[string]any{}}); got.Error != nil || got.Result.IsError || w.agency.calls != 1 {
		t.Errorf("list_clients = %+v %+v", got.Error, got.Result)
	}
}

func TestAgencyMCPWithHeaderActsAsThatClient(t *testing.T) {
	w := newMCPWorld(t, models.APIPermAgencyRead|models.APIPermReadCampaigns)
	_, got := w.rpc(t, w.client.String(), "tools/list", nil)
	tools := toolNames(got)
	if _, ok := tools["list_clients"]; ok {
		t.Error("agency tools listed on a client-bound session")
	}
	schema, ok := tools["read_campaigns"]
	if !ok {
		t.Fatalf("workspace tool missing: %v", tools)
	}
	if props, _ := schema["properties"].(map[string]any); props["organization_id"] != nil {
		t.Error("organization_id injected on a client-bound session")
	}
	_, got = w.rpc(t, w.client.String(), "tools/call", map[string]any{"name": "read_campaigns", "arguments": map[string]any{"q": "acme"}})
	if got.Error != nil || w.seen == nil || w.seen.OrgID != w.client || w.seen.AgencyKeyID == uuid.Nil {
		t.Fatalf("header call = %+v, invocation %+v", got.Error, w.seen)
	}
	if code, _ := w.rpc(t, uuid.NewString(), "tools/list", nil); code != http.StatusForbidden {
		t.Errorf("header naming a non-client = %d, want 403", code)
	}
}

// Workspace credentials reach /mcp exactly as before: their own workspace, no agency tools.
func TestMCPWorkspaceCallerUnchanged(t *testing.T) {
	w := newMCPWorld(t, models.APIPermAgencyRead)
	org, user := uuid.New(), uuid.New()
	member := &models.OrganizationMember{OrganizationID: org, UserID: user, Permissions: models.PermViewCampaigns}
	h := &Handler{AITools: aitools.NewRegistry(), AgencyTools: aitools.BuildAgencyRegistry(aitools.AgencyDeps{Agency: w.agency})}
	var seen *aitools.Invocation
	h.AITools.Register(aitools.Tool{
		Name: "read_campaigns", Risk: generation.RiskRead, RequiredAPIPerm: models.APIPermReadCampaigns, RequiredOrgPerm: models.PermViewCampaigns,
		Handler: func(_ context.Context, inv aitools.Invocation, _ json.RawMessage) (string, error) {
			seen = &inv
			return "{}", nil
		},
	})
	for _, authType := range []string{middleware.AuthTypeAPIKey, middleware.AuthTypeOAuth} {
		r := gin.New()
		r.POST("/v1/mcp", func(c *gin.Context) {
			c.Set(middleware.AuthTypeKey, authType)
			c.Set(middleware.OrganizationIDKey, org)
			c.Set(middleware.UserIDKey, user.String())
			c.Set(middleware.APIKeyPermissionsKey, models.APIPermReadCampaigns|models.APIPermAgencyRead)
			c.Set(middleware.SessionMemberKey, member)
		}, h.MCPEndpoint)
		w.r = r
		_, got := w.rpc(t, "", "tools/list", nil)
		if tools := toolNames(got); len(tools) != 1 || tools["read_campaigns"] == nil {
			t.Errorf("%s tools/list = %v", authType, tools)
		}
		seen = nil
		_, got = w.rpc(t, "", "tools/call", map[string]any{"name": "list_clients", "arguments": map[string]any{}})
		if got.Error == nil || w.agency.calls != 0 {
			t.Errorf("%s reached an agency tool", authType)
		}
		_, got = w.rpc(t, "", "tools/call", map[string]any{"name": "read_campaigns", "arguments": map[string]any{}})
		if got.Error != nil || seen == nil || seen.OrgID != org || !seen.ActsForMember || seen.AgencyKeyID != uuid.Nil {
			t.Errorf("%s call = %+v, invocation %+v", authType, got.Error, seen)
		}
	}
}
