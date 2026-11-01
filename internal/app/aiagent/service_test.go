package aiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

func testRegistry() *aitools.Registry {
	r := aitools.NewRegistry()
	noop := func(context.Context, aitools.Invocation, json.RawMessage) (string, error) { return `{}`, nil }
	r.Register(aitools.Tool{Name: "add_tag", Risk: generation.RiskWrite, RequiredOrgPerm: models.PermManageContacts, Handler: noop})
	r.Register(aitools.Tool{Name: "set_campaign_status", Risk: generation.RiskWrite, AlwaysAsk: true, RequiredOrgPerm: models.PermSendCampaigns, Handler: noop})
	r.Register(aitools.Tool{Name: "send_reply", Risk: generation.RiskSend, RequiredOrgPerm: models.PermAccessUnibox, Handler: noop})
	r.Register(aitools.Tool{Name: "list_contacts", Risk: generation.RiskRead, RequiredOrgPerm: models.PermViewContacts, Handler: noop})
	return r
}

func TestCanonicalArgsSortedAndCapped(t *testing.T) {
	got, cut := canonicalArgs(json.RawMessage(`{"b":1,"a":{"d":2,"c":3}}`))
	if cut {
		t.Fatal("small args were cut")
	}
	if !(strings.Index(got, `"a"`) < strings.Index(got, `"b"`) && strings.Index(got, `"c"`) < strings.Index(got, `"d"`)) {
		t.Fatalf("keys not sorted: %s", got)
	}
	big := `{"body":"` + strings.Repeat("é", models.AgentApprovalArgsMax) + `"}`
	got, cut = canonicalArgs(json.RawMessage(big))
	if !cut || len(got) > models.AgentApprovalArgsMax {
		t.Fatalf("large args: cut=%v len=%d", cut, len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("cut inside a rune: %q", got[len(got)-8:])
	}
}

func TestWithholdSecrets(t *testing.T) {
	out, secrets := withholdSecrets(`{"ok":true,"secret":"whsec_abc","webhook_id":"w"}`, []string{"secret"})
	if secrets["secret"] != "whsec_abc" {
		t.Fatalf("secret not returned: %v", secrets)
	}
	if strings.Contains(out, "whsec_abc") || !strings.Contains(out, `"webhook_id":"w"`) {
		t.Fatalf("model-facing result = %s", out)
	}
	if out, secrets := withholdSecrets(`{"error":"nope"}`, []string{"secret"}); secrets != nil || out != `{"error":"nope"}` {
		t.Fatalf("a result without the field changed: %s %v", out, secrets)
	}
}

func TestAlwaysAllowNeedsSettingsAndANonSendingTool(t *testing.T) {
	s := &service{registry: testRegistry()}
	admin := aitools.Invocation{OrgID: uuid.New(), OrgPerms: models.PermManageSettings | models.PermManageContacts}
	member := aitools.Invocation{OrgID: uuid.New(), OrgPerms: models.PermManageContacts}
	if !s.alwaysAllowOffered(admin, "add_tag", "write") {
		t.Error("a settings manager is not offered always allow on a write tool")
	}
	if s.alwaysAllowOffered(member, "add_tag", "write") {
		t.Error("a member without manage_settings is offered always allow")
	}
	for _, name := range []string{"set_campaign_status", "send_reply", "mcp_x_y", "unknown"} {
		if s.alwaysAllowOffered(admin, name, "write") {
			t.Errorf("%s is offered always allow", name)
		}
	}
	policies := map[string]string{"add_tag": "always_allow", "set_campaign_status": "always_allow", "mcp_x_y": "always_allow"}
	if !s.autoAllowed(policies, generation.ToolDef{Name: "add_tag", Risk: generation.RiskWrite}) {
		t.Error("a policy for a write tool does not apply")
	}
	for _, name := range []string{"set_campaign_status", "mcp_x_y"} {
		if s.autoAllowed(policies, generation.ToolDef{Name: name, Risk: generation.RiskWrite}) {
			t.Errorf("a policy auto-runs %s", name)
		}
	}
}

func TestForReaderWithholdsResultsOfToolsTheReaderCannotRun(t *testing.T) {
	s := &service{registry: testRegistry()}
	reader := aitools.Invocation{OrgID: uuid.New(), OrgPerms: models.PermViewContacts}
	msgs := []generation.AgentMessage{
		{Role: "assistant", ToolCalls: []generation.ToolCall{{ID: "1", Name: "list_contacts"}, {ID: "2", Name: "send_reply"}}},
		{Role: "tool", ToolCallID: "1", Content: `{"count":1}`},
		{Role: "tool", ToolCallID: "2", Content: `{"to":"someone@example.com"}`},
	}
	out := s.forReader(context.Background(), reader, msgs)
	if out[1].Content != `{"count":1}` {
		t.Errorf("a permitted result was withheld: %s", out[1].Content)
	}
	if out[2].Content != withheldResult {
		t.Errorf("an unpermitted result was kept: %s", out[2].Content)
	}
	if msgs[2].Content == withheldResult {
		t.Error("the stored messages were modified")
	}
}

// TestTranscriptOnlyDescribesDraftToolsAsCreation keeps read results from being presented as newly created resources.
func TestTranscriptOnlyDescribesDraftToolsAsCreation(t *testing.T) {
	for _, tc := range []struct{ tool, result, want string }{
		{"get_campaign_stats", `{"campaign_id":"c1","emails_sent":4}`, `{"campaign_id":"c1","emails_sent":4}`},
		{"get_automation", `{"automation_id":"a1"}`, `{"automation_id":"a1"}`},
		{"create_campaign_draft", `{"campaign_id":"c1"}`, "created campaign c1"},
		{"create_automation_draft", `{"automation_id":"a1"}`, "created automation a1"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			repo := &runRepository{session: models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()}}
			for _, message := range []generation.AgentMessage{
				{Role: "assistant", ToolCalls: []generation.ToolCall{{ID: "call", Name: tc.tool}}},
				{Role: "tool", ToolCallID: "call", Content: tc.result},
			} {
				content, err := json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				repo.rows = append(repo.rows, models.AgentMessageRow{Role: message.Role, Content: content})
			}
			svc := NewService(repo, nil, nil, nil, nil, nil, nil, nil, nil)
			turns, _, xerr := svc.Transcript(context.Background(), aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}, repo.session.ID)
			if xerr != nil {
				t.Fatal(xerr)
			}
			if got := turns[0].Blocks[0].Result; got != tc.want {
				t.Errorf("tool summary = %q, want %q", got, tc.want)
			}
		})
	}
}

type transcriptRepo struct {
	repository.AgentRepository
	rows []models.AgentMessageRow
}

func (r transcriptRepo) LoadTranscript(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]models.AgentMessageRow, error) {
	return r.rows, nil
}

func TestLoadTranscriptAnswersEveryToolCallInPlace(t *testing.T) {
	asked := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	ran := asked.Add(time.Minute)
	row := func(at time.Time, m generation.AgentMessage) models.AgentMessageRow {
		b, _ := json.Marshal(m)
		return models.AgentMessageRow{Role: m.Role, Content: b, CreatedAt: at}
	}
	call := func(id string) generation.AgentMessage {
		return generation.AgentMessage{Role: "assistant", ToolCalls: []generation.ToolCall{{ID: id, Name: "list_contacts"}}}
	}
	result := func(id string) generation.AgentMessage {
		return generation.AgentMessage{Role: "tool", ToolCallID: id, Content: id}
	}
	// A call paused for approval, then the resumed run older releases saved under one timestamp, read back shuffled.
	s := &service{repo: transcriptRepo{rows: []models.AgentMessageRow{
		row(asked, generation.AgentMessage{Role: "user", Content: "find leads"}),
		row(asked.Add(30*time.Second), call("a")),
		row(ran, generation.AgentMessage{Role: "assistant", Content: "done"}),
		row(ran, result("b")),
		row(ran, call("c")),
		row(ran, result("c")),
		row(ran, result("a")),
		row(ran, call("b")),
	}}}
	got, xerr := s.loadTranscript(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(got) != 8 || got[0].Content != "find leads" || got[1].ToolCalls[0].ID != "a" {
		t.Fatalf("earlier rows moved: %v", got)
	}
	if got[2].ToolCallID != "a" {
		t.Fatalf("result for the approved call is not first: %v", got)
	}
	if got[7].Content != "done" {
		t.Fatalf("reply is not last: %v", got)
	}
	for i := 3; i < 7; i += 2 {
		if len(got[i].ToolCalls) != 1 || got[i+1].ToolCallID != got[i].ToolCalls[0].ID {
			t.Fatalf("tool call at %d is not followed by its result: %v", i, got)
		}
	}
}
