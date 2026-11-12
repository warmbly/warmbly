package aitools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/subscription"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

type catalogMCPSource struct {
	count int
}

type catalogSlack struct{ SlackControl }
type catalogPlacement struct{ PlacementTests }
type catalogAdvisor struct{ AdvisorReader }
type catalogSequences struct{ sequence.SequenceService }
type catalogSubscription struct {
	subscription.SubscriptionService
}

func (s catalogMCPSource) ToolsForInvocation(context.Context, Invocation) []generation.ToolDef {
	tools := make([]generation.ToolDef, s.count)
	for i := range tools {
		tools[i] = generation.ToolDef{Name: fmt.Sprintf("mcp_crm_tool_%d", i), Risk: generation.RiskRead, InputSchema: map[string]any{"type": "object"}}
	}
	return tools
}

func TestRealRegistryStaysWithinOpenAIToolLimit(t *testing.T) {
	for _, mcpCount := range []int{0, 300} {
		t.Run(fmt.Sprintf("mcp=%d", mcpCount), func(t *testing.T) {
			r := BuildRegistry(Deps{
				Skills: stubSkillLookup{}, Emails: stubEmails{}, Org: stubOrg{}, APIKeys: stubAPIKeys{}, Webhooks: stubWebhooks{},
				Segments: stubSegments{}, Forms: stubForms{}, Suppressions: stubSuppressions{},
				Sequences: catalogSequences{}, Subscription: catalogSubscription{},
			})
			RegisterSlackTools(r, catalogSlack{}, nil)
			RegisterPlacementTools(r, catalogPlacement{}, nil)
			RegisterAdvisorTools(r, catalogAdvisor{})
			r.AddDynamicSource(catalogMCPSource{count: mcpCount})
			inv := Invocation{OrgID: uuid.New(), OrgPerms: models.RolePermissions[models.RoleAdmin]}
			defs := r.ToolDefs(context.Background(), inv)
			if len(defs) <= 128 {
				t.Fatalf("regression requires the oversized built-in registry, got %d tools", len(defs))
			}
			core := 0
			for _, d := range defs {
				if !d.Deferred {
					core++
					if !CoreTools[d.Name] {
						t.Fatalf("%s is on the wire but not a core tool", d.Name)
					}
				}
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Tools []json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				requests++
				if len(req.Tools) != core+1 || len(req.Tools) > 40 {
					t.Errorf("initial OpenAI request includes %d tools instead of the %d core tools and discovery", len(req.Tools), core)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hello!"}}]}`))
			}))
			defer srv.Close()
			provider, err := generation.NewProvider(generation.ProviderConfig{OpenAIAPIKey: "test-key", OpenAIBaseURL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			result, err := provider.RunAgent(context.Background(), generation.AgentRequest{Tools: defs, Messages: []generation.AgentMessage{{Role: "user", Content: "Hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != "Hello!" || requests != 1 {
				t.Fatalf("greeting = %+v, requests = %d", result, requests)
			}
		})
	}
}
