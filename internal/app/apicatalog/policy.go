package apicatalog

import "strings"

// blockedAreas are API areas an agent never calls through the bridge. Each has
// a dashboard flow or a dedicated tool that keeps its secrets out of the conversation.
var blockedAreas = []struct{ prefix, reason string }{
	{"/auth", "sign-in, sessions and account security belong to the member, in the dashboard"},
	{"/oauth", "OAuth applications hold client secrets; manage them in the dashboard"},
	{"/api-keys", "use the API key tools, which keep a new key out of the conversation"},
	{"/webhooks", "use the webhook tools, which keep the signing secret out of the conversation"},
	{"/ai/tools", "this is the tool surface itself"},
}

// sendOperations put mail on the wire or start sending. They run only through
// the dedicated tools, which show the sender and recipients before anything goes out.
var sendOperations = map[string]string{
	"POST /emails/{id}/send":                                 "sends mail; use compose_email",
	"POST /unibox/reply":                                     "sends mail; use send_reply, or draft_reply to draft",
	"POST /unibox/compose":                                   "sends mail; use compose_email",
	"POST /unibox/agent-drafts/{id}/approve":                 "sends an inbox agent draft; approve it in the inbox",
	"POST /campaigns/{id}/test-email":                        "sends mail; send a test from the campaign editor",
	"POST /campaigns/{id}/start":                             "starts sending; use set_campaign_status",
	"POST /placement/tests":                                  "sends to seed inboxes; use run_placement_test",
	"POST /placement/batches":                                "sends to seed inboxes; use run_placement_batch",
	"POST /integrations/connections/{id}/rotate-inbound-url": "returns a secret inbound URL; rotate it in the dashboard",
}

// credentialOperations take a credential in a free-form body the schema cannot show.
var credentialOperations = map[string]bool{
	"POST /emails/vendors":           true,
	"PATCH /emails/vendors/{id}":     true,
	"POST /integrations/connections": true,
}

const credentialReason = "takes a credential; enter it in the dashboard, where it is sealed rather than kept in a conversation"

// readPosts take their filters in a body but change nothing, so they run without approval.
var readPosts = map[string]bool{
	"POST /contacts/search":                             true,
	"POST /crm/deals/search":                            true,
	"POST /crm/deals/summary":                           true,
	"POST /crm/tasks/search":                            true,
	"POST /crm/tasks/summary":                           true,
	"POST /segments/preview":                            true,
	"POST /segments/{id}/members/lookup":                true,
	"POST /leads/query":                                 true,
	"POST /leads/facets":                                true,
	"POST /leads/points":                                true,
	"POST /campaign-template-preview":                   true,
	"POST /campaigns-estimate":                          true,
	"POST /campaigns/{id}/preflight":                    true,
	"POST /placement/batches/preview":                   true,
	"POST /templates/score":                             true,
	"POST /templates/{id}/render":                       true,
	"POST /emails/imports/preview":                      true,
	"POST /crm/lists/preview":                           true,
	"POST /lead-sync/google/preview":                    true,
	"POST /integrations/salesforce/{id}/import/preview": true,
	"POST /integrations/apollo/{id}/import/preview":     true,
}

// classify decides whether an operation reads or writes, and whether an agent may call it at all.
func (c *Catalog) classify(o *Operation) (Kind, string) {
	kind := KindWrite
	if o.Method == "GET" || readPosts[o.Key()] {
		kind = KindRead
	}
	for _, a := range blockedAreas {
		if o.Path == a.prefix || strings.HasPrefix(o.Path, a.prefix+"/") {
			return kind, a.reason
		}
	}
	if reason, ok := sendOperations[o.Key()]; ok {
		return kind, reason
	}
	if sec, ok := o.raw["security"].([]any); ok && len(sec) == 0 {
		return kind, "a public endpoint with no workspace context"
	}
	if body, ok := o.raw["requestBody"].(map[string]any); ok {
		content, _ := body["content"].(map[string]any)
		if _, json := content["application/json"]; !json && len(content) > 0 {
			return kind, "takes a file upload; upload it in the dashboard"
		}
		if credentialOperations[o.Key()] || c.takesSecret(jsonSchema(body), 5, nil) {
			return kind, credentialReason
		}
	}
	return kind, ""
}

// takesSecret reports whether a request schema has a credential-shaped property anywhere within depth.
func (c *Catalog) takesSecret(v any, depth int, stack []string) bool {
	switch x := v.(type) {
	case map[string]any:
		if ref := str(x["$ref"]); strings.HasPrefix(ref, "#/components/schemas/") {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			for _, s := range stack {
				if s == name {
					return false
				}
			}
			return depth > 0 && c.takesSecret(c.schemas[name], depth-1, append(stack, name))
		}
		if props, ok := x["properties"].(map[string]any); ok {
			for k := range props {
				if SecretField(k) {
					return true
				}
			}
		}
		for _, e := range x {
			if c.takesSecret(e, depth, stack) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if c.takesSecret(e, depth, stack) {
				return true
			}
		}
	}
	return false
}

// SecretField reports whether a JSON field name holds a credential.
func SecretField(name string) bool {
	k := strings.ToLower(name)
	switch k {
	case "secret", "password", "token", "api_key", "apikey", "key_value", "raw_key", "credentials", "private_key", "inbound_url", "signing_key":
		return true
	}
	for _, suf := range []string{"_secret", "_password", "_token", "_api_key", "_private_key", "_credentials", "_signing_key"} {
		if strings.HasSuffix(k, suf) {
			return true
		}
	}
	return false
}
