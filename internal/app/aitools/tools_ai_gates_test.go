package aitools

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/models"
)

type stubSkillLookup struct{ SkillLookup }
type stubEmails struct{ email.EmailService }
type stubOrg struct {
	organization.OrganizationService
}
type stubAPIKeys struct{ apikey.APIKeyService }
type stubWebhooks struct{ webhook.Service }

func aiGateRegistry() *Registry {
	return BuildRegistry(Deps{
		Skills: stubSkillLookup{}, Emails: stubEmails{}, Org: stubOrg{}, APIKeys: stubAPIKeys{}, Webhooks: stubWebhooks{},
		Segments: stubSegments{}, Forms: stubForms{}, Suppressions: stubSuppressions{},
	})
}

// The web and playbook tools act for the assistant, so they need the assistant's own permission.
func TestResearchToolsRequireAIAccess(t *testing.T) {
	r := aiGateRegistry()
	org := uuid.New()
	for _, name := range []string{"search_web", "fetch_url", "load_skill"} {
		if r.Permits(Invocation{OrgID: org, OrgPerms: models.PermViewContacts}, name) {
			t.Errorf("%s: a member without use_ai may run it", name)
		}
		if !r.Permits(Invocation{OrgID: org, OrgPerms: models.PermUseAI}, name) {
			t.Errorf("%s: a member with use_ai may not run it", name)
		}
		if r.Permits(Invocation{OrgID: org, IsAPIKey: true, APIPerms: models.APIPermReadContacts}, name) {
			t.Errorf("%s: a key without AI_AGENT may run it", name)
		}
		if !r.Permits(Invocation{OrgID: org, IsAPIKey: true, APIPerms: models.APIPermAIAgent}, name) {
			t.Errorf("%s: a key with AI_AGENT may not run it", name)
		}
	}
}

// Feature agents bind the research tools under their own gate, and nothing else.
func TestResearchToolsBindOnlyResearch(t *testing.T) {
	r := aiGateRegistry()
	defs := r.ResearchTools(Invocation{OrgID: uuid.New()}, "search_web", "fetch_url", "load_skill", "list_contacts", "set_campaign_status")
	got := map[string]bool{}
	for _, d := range defs {
		got[d.Name] = true
	}
	if len(got) != 3 || !got["search_web"] || !got["fetch_url"] || !got["load_skill"] {
		t.Fatalf("ResearchTools bound %v", got)
	}
	if n := len(r.WebResearchTools(uuid.New())); n != 2 {
		t.Fatalf("WebResearchTools bound %d tools, want 2", n)
	}
}

// Starting sending and changing who has access always ask, whatever a workspace policy says.
func TestAlwaysAskTools(t *testing.T) {
	r := aiGateRegistry()
	for _, name := range []string{
		"set_campaign_status", "set_automation_enabled", "set_mailbox_warmup", "set_mailbox_send_hold",
		"set_lead_hold", "set_campaign_segments", "add_segment_to_campaign",
		"invite_member", "update_member_role", "remove_member", "create_api_key", "update_api_key", "revoke_api_key",
	} {
		tool, ok := r.Get(name)
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if !tool.AlwaysAsk {
			t.Errorf("%s can be always-allowed", name)
		}
	}
}

// A tool whose REST route needs a fresh confirmation needs the same one, for a session; an API key passes.
func TestFreshAuthTools(t *testing.T) {
	r := aiGateRegistry()
	for _, name := range []string{
		"invite_member", "update_member_role", "remove_member", "create_api_key", "update_api_key",
		"create_webhook", "update_webhook", "rotate_webhook_secret",
	} {
		tool, ok := r.Get(name)
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if !tool.FreshAuth {
			t.Errorf("%s runs without a fresh confirmation", name)
			continue
		}
		if _, err := tool.run(context.Background(), Invocation{}, nil); err != ErrToolNeedsFreshAuth {
			t.Errorf("%s on a stale session = %v", name, err)
		}
	}
}

// Tools that return a secret declare it, so the assistant never holds it.
func TestSecretFieldsDeclared(t *testing.T) {
	r := aiGateRegistry()
	for name, field := range map[string]string{"get_invitation_link": "token", "create_webhook": "secret", "rotate_webhook_secret": "secret"} {
		tool, ok := r.Get(name)
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if len(tool.SecretFields) != 1 || tool.SecretFields[0] != field {
			t.Errorf("%s secret fields = %v, want [%s]", name, tool.SecretFields, field)
		}
	}
}

// A preview that is not permitted to the caller resolves nothing.
func TestPreviewSendNeedsPermission(t *testing.T) {
	r := aiGateRegistry()
	p, pinned, err := r.PreviewSend(context.Background(), Invocation{OrgID: uuid.New()}, "send_reply", []byte(`{"thread_id":"x","body":"hi"}`))
	if p != nil || pinned != nil || err != nil {
		t.Fatalf("preview for an unpermitted caller = %v %s %v", p, pinned, err)
	}
}
