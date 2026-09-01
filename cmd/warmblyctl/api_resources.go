package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// bodyMode says what a typed command does with --data.
type bodyMode int

const (
	bodyNone     bodyMode = iota // the endpoint takes no body
	bodyOptional                 // --data may be given; an empty one is sent as {}
	bodyRequired                 // --data must be given
)

// apiSpec is one typed command over the public API. The table below is the
// entire definition of the resource commands: dispatch, flags, help and the
// request all come from it, so adding an endpoint is adding a row.
type apiSpec struct {
	name    string   // "campaign start"
	summary string   // one line for help output
	method  string   // HTTP method
	path    string   // /v1-relative path, may contain {id} and {child}
	body    bodyMode // what --data means here
	child   string   // flag name filling {child}, e.g. "step"
	query   []string // query parameters exposed as flags
	sends   bool     // true when the command can put real mail on the wire
	idLabel string   // what {id} is called in help, when it is not a uuid
}

// idPlaceholder is what the generated help shows for --id. Most resources are
// uuid-keyed; the ones that are not say so, since a user who copies the
// example verbatim would otherwise send a malformed identifier.
func (s apiSpec) idPlaceholder() string {
	if s.idLabel != "" {
		return "<" + s.idLabel + ">"
	}
	return "<uuid>"
}

var apiSpecs = []apiSpec{
	{name: "me", summary: "Show who the API key is: user, organization, and granted scopes", method: "GET", path: "/me"},

	// Campaigns.
	{name: "campaign list", summary: "List campaigns", method: "GET", path: "/campaigns", query: []string{"limit", "cursor", "q", "status", "folder"}},
	{name: "campaign get", summary: "Get one campaign", method: "GET", path: "/campaigns/{id}"},
	{name: "campaign overview", summary: "Status and folder counts across all campaigns", method: "GET", path: "/campaigns-overview"},
	{name: "campaign create", summary: "Create a campaign", method: "POST", path: "/campaigns", body: bodyRequired},
	{name: "campaign update", summary: "Update a campaign", method: "PATCH", path: "/campaigns/{id}", body: bodyRequired},
	{name: "campaign delete", summary: "Delete a campaign", method: "DELETE", path: "/campaigns/{id}"},
	{name: "campaign steps", summary: "List a campaign's sequence steps", method: "GET", path: "/campaigns/{id}/steps"},
	{name: "campaign add-step", summary: "Add a sequence step", method: "POST", path: "/campaigns/{id}/steps", body: bodyRequired},
	{name: "campaign update-step", summary: "Update a sequence step", method: "PATCH", path: "/campaigns/{id}/steps/{child}", body: bodyRequired, child: "step"},
	{name: "campaign delete-step", summary: "Delete a sequence step", method: "DELETE", path: "/campaigns/{id}/steps/{child}", child: "step"},
	{name: "campaign senders", summary: "Show the campaign's sender pool and weights", method: "GET", path: "/campaigns/{id}/senders"},
	{name: "campaign set-senders", summary: "Replace the campaign's sender pool", method: "PUT", path: "/campaigns/{id}/senders", body: bodyRequired},
	{name: "campaign preflight", summary: "Run the pre-send checks without sending", method: "POST", path: "/campaigns/{id}/preflight"},
	{name: "campaign test-email", summary: "Send the campaign as a test to an address you name", method: "POST", path: "/campaigns/{id}/test-email", body: bodyRequired, sends: true},
	{name: "campaign start", summary: "Start the campaign. This sends real mail", method: "POST", path: "/campaigns/{id}/start", sends: true},
	{name: "campaign stop", summary: "Stop the campaign", method: "POST", path: "/campaigns/{id}/stop"},
	{name: "campaign logs", summary: "The campaign's send log", method: "GET", path: "/campaigns/{id}/logs", query: []string{"limit", "cursor"}},

	// Contacts.
	{name: "contact list", summary: "List or search contacts; --data carries the filter body", method: "POST", path: "/contacts/search", body: bodyOptional, query: []string{"limit", "cursor"}},
	{name: "contact get", summary: "Get one contact with suppression state", method: "GET", path: "/contacts/{id}"},
	{name: "contact lookup", summary: "Resolve a contact by email address", method: "GET", path: "/contacts/lookup", query: []string{"email"}},
	{name: "contact create", summary: "Create a contact", method: "POST", path: "/contacts", body: bodyRequired},
	{name: "contact update", summary: "Update a contact", method: "PATCH", path: "/contacts/{id}", body: bodyRequired},
	{name: "contact delete", summary: "Delete a contact", method: "DELETE", path: "/contacts/{id}"},
	{name: "contact timeline", summary: "Everything that happened to a contact, newest first", method: "GET", path: "/contacts/{id}/timeline", query: []string{"limit", "cursor"}},
	{name: "contact emails", summary: "Emails sent to a contact", method: "GET", path: "/contacts/{id}/emails", query: []string{"limit", "cursor"}},
	{name: "contact notes", summary: "List a contact's notes", method: "GET", path: "/contacts/{id}/notes"},
	{name: "contact add-note", summary: "Add a note to a contact", method: "POST", path: "/contacts/{id}/notes", body: bodyRequired},
	{name: "contact custom-fields", summary: "The distinct custom field keys in use", method: "GET", path: "/contacts/custom-fields"},
	{name: "contact import-preview", summary: "Preview a bulk import without writing", method: "POST", path: "/contacts/import/preview", body: bodyRequired},
	{name: "contact import-commit", summary: "Commit a previewed bulk import", method: "POST", path: "/contacts/import/commit", body: bodyRequired},
	{name: "contact export", summary: "Export contacts", method: "POST", path: "/contacts/export", body: bodyOptional},

	// Mailboxes (email accounts).
	{name: "mailbox list", summary: "List connected mailboxes", method: "GET", path: "/emails", query: []string{"limit", "cursor", "q"}},
	{name: "mailbox get", summary: "Get one mailbox", method: "GET", path: "/emails/{id}"},
	{name: "mailbox update", summary: "Update mailbox settings (limits, tags, timezone, signature)", method: "PATCH", path: "/emails/{id}", body: bodyRequired},
	{name: "mailbox delete", summary: "Disconnect a mailbox", method: "DELETE", path: "/emails/{id}"},
	{name: "mailbox auth-check", summary: "Check the mailbox's SPF, DKIM and DMARC", method: "GET", path: "/emails/{id}/auth-check"},
	{name: "mailbox sync", summary: "The mailbox's sync state and backfill progress", method: "GET", path: "/emails/{id}/sync"},
	{name: "mailbox behavior", summary: "The mailbox's human-sending ranges", method: "GET", path: "/emails/{id}/behavior"},
	{name: "mailbox set-behavior", summary: "Update the mailbox's sending behaviour", method: "PUT", path: "/emails/{id}/behavior", body: bodyRequired},
	{name: "mailbox verify", summary: "Verify an email address without sending", method: "POST", path: "/emails/verify", body: bodyRequired},
	{name: "mailbox send", summary: "Send one email from this mailbox. This sends real mail", method: "POST", path: "/emails/{id}/send", body: bodyRequired, sends: true},
	{name: "mailbox warmup-start", summary: "Start warming the mailbox", method: "POST", path: "/emails/{id}/warmup/start"},
	{name: "mailbox warmup-pause", summary: "Pause warmup", method: "POST", path: "/emails/{id}/warmup/pause"},
	{name: "mailbox warmup-resume", summary: "Resume warmup", method: "POST", path: "/emails/{id}/warmup/resume"},
	{name: "mailbox warmup-stop", summary: "Stop warmup", method: "POST", path: "/emails/{id}/warmup/stop"},
	{name: "mailbox warmup-status", summary: "The mailbox's warmup pool ban status", method: "GET", path: "/emails/{id}/warmup/ban-status"},
	{name: "mailbox hold", summary: "Hold the mailbox out of campaign sending", method: "POST", path: "/emails/{id}/hold"},
	{name: "mailbox release", summary: "Put a held mailbox back into campaign sending", method: "POST", path: "/emails/{id}/release"},

	// Unified inbox.
	{name: "inbox list", summary: "List inbox messages", method: "GET", path: "/unibox", query: []string{"limit", "cursor", "address", "direction", "folder", "from", "subject", "unseen", "awaiting_reply", "since", "until"}},
	{name: "inbox count", summary: "The unseen message count", method: "GET", path: "/unibox/count"},
	{name: "inbox overview", summary: "Per-mailbox and per-tag inbox rollup", method: "GET", path: "/unibox/overview"},
	{name: "inbox thread", summary: "One conversation thread", method: "GET", path: "/unibox/thread", query: []string{"thread_id", "email_id", "limit", "cursor"}},
	{name: "inbox seen", summary: "Mark messages seen or unseen", method: "PATCH", path: "/unibox/seen", body: bodyRequired},
	{name: "inbox reply", summary: "Reply in a thread. This sends real mail", method: "POST", path: "/unibox/reply", body: bodyRequired, sends: true},
	{name: "inbox compose", summary: "Compose a new email. This sends real mail", method: "POST", path: "/unibox/compose", body: bodyRequired, sends: true},
	{name: "inbox drafts", summary: "List the AI agent's drafts awaiting approval", method: "GET", path: "/unibox/agent-drafts"},
	{name: "inbox approve-draft", summary: "Approve an agent draft, which sends it", method: "POST", path: "/unibox/agent-drafts/{id}/approve", sends: true},
	{name: "inbox discard-draft", summary: "Discard an agent draft", method: "POST", path: "/unibox/agent-drafts/{id}/discard"},
	{name: "inbox scheduled", summary: "List scheduled sends", method: "GET", path: "/unibox/scheduled"},
	{name: "inbox cancel-scheduled", summary: "Cancel a scheduled send", method: "DELETE", path: "/unibox/scheduled/{id}"},

	// Analytics and audit.
	{name: "analytics dashboard", summary: "The dashboard numbers", method: "GET", path: "/analytics/dashboard"},
	{name: "analytics deliverability", summary: "Bounces, complaints and placement", method: "GET", path: "/analytics/deliverability"},
	{name: "analytics warmup", summary: "Warmup analytics", method: "GET", path: "/analytics/warmup"},
	{name: "analytics accounts", summary: "Per-mailbox analytics", method: "GET", path: "/analytics/accounts"},
	{name: "analytics account", summary: "One mailbox's analytics", method: "GET", path: "/analytics/accounts/{id}"},
	{name: "analytics campaign", summary: "One campaign's analytics", method: "GET", path: "/analytics/campaigns/{id}"},
	{name: "analytics campaign-daily", summary: "One campaign's daily series", method: "GET", path: "/analytics/campaigns/{id}/daily"},
	{name: "analytics campaign-hourly", summary: "One campaign's hourly series", method: "GET", path: "/analytics/campaigns/{id}/hourly"},
	{name: "analytics usage", summary: "API and plan usage", method: "GET", path: "/analytics/usage"},
	{name: "analytics audit-logs", summary: "The organization's audit trail", method: "GET", path: "/audit-logs", query: []string{"limit", "cursor"}},

	// Organization-wide sending settings.
	{name: "settings outreach", summary: "The organization's outreach and suppression settings", method: "GET", path: "/outreach/settings"},
	{name: "settings set-outreach", summary: "Update the outreach settings", method: "PATCH", path: "/outreach/settings", body: bodyRequired},

	// Webhooks.
	{name: "webhook list", summary: "List webhook endpoints", method: "GET", path: "/webhooks"},
	{name: "webhook create", summary: "Create a webhook endpoint", method: "POST", path: "/webhooks", body: bodyRequired},
	{name: "webhook update", summary: "Update a webhook endpoint", method: "PATCH", path: "/webhooks/{id}", body: bodyRequired},
	{name: "webhook delete", summary: "Delete a webhook endpoint", method: "DELETE", path: "/webhooks/{id}"},
	{name: "webhook verify", summary: "Send a verification ping to the endpoint", method: "POST", path: "/webhooks/{id}/verify"},
	{name: "webhook rotate-secret", summary: "Rotate the endpoint's signing secret", method: "POST", path: "/webhooks/{id}/rotate-secret"},
	{name: "webhook deliveries", summary: "Recent deliveries across endpoints", method: "GET", path: "/webhooks/deliveries", query: []string{"limit", "cursor"}},
	{name: "webhook event-types", summary: "Every event type a webhook can subscribe to", method: "GET", path: "/webhooks/event-types"},

	// API keys (self-service).
	{name: "apikey list", summary: "List the organization's API keys", method: "GET", path: "/api-keys"},
	{name: "apikey get", summary: "Get one API key", method: "GET", path: "/api-keys/{id}"},
	{name: "apikey create", summary: "Create an API key; the secret is only ever in this response", method: "POST", path: "/api-keys", body: bodyRequired},
	{name: "apikey update", summary: "Update an API key's name, scopes or restrictions", method: "PATCH", path: "/api-keys/{id}", body: bodyRequired},
	{name: "apikey revoke", summary: "Revoke an API key", method: "DELETE", path: "/api-keys/{id}"},
	{name: "apikey permissions", summary: "Every grantable scope with its bit value", method: "GET", path: "/api-keys/permissions"},

	// Templates.
	{name: "template list", summary: "List reply templates", method: "GET", path: "/templates"},
	{name: "template get", summary: "Get one template", method: "GET", path: "/templates/{id}"},
	{name: "template create", summary: "Create a template", method: "POST", path: "/templates", body: bodyRequired},
	{name: "template update", summary: "Update a template", method: "PATCH", path: "/templates/{id}", body: bodyRequired},
	{name: "template delete", summary: "Delete a template", method: "DELETE", path: "/templates/{id}"},

	// CRM.
	{name: "crm pipelines", summary: "List pipelines with their stages", method: "GET", path: "/crm/pipelines"},
	{name: "crm deals", summary: "Search deals; --data carries the filter body", method: "POST", path: "/crm/deals/search", body: bodyOptional},
	{name: "crm tasks", summary: "Search CRM tasks; --data carries the filter body", method: "POST", path: "/crm/tasks/search", body: bodyOptional},

	// Agent tools: the shared AI tool registry over REST, for function-calling
	// agents that do not speak MCP. --id is the tool name, not a UUID.
	{name: "tool list", summary: "List the tools this key may call; --format openai emits function-calling manifests", method: "GET", path: "/ai/tools", query: []string{"format"}},
	{name: "tool call", summary: "Execute one registry tool; --data carries its JSON argument object", method: "POST", path: "/ai/tools/{id}/call", body: bodyOptional, idLabel: "tool-name"},
}

// apiFamilyOrder keeps the top-level help stable; maps iterate randomly.
var apiFamilyOrder = []string{
	"me", "campaign", "contact", "mailbox", "inbox", "analytics",
	"settings", "webhook", "apikey", "template", "crm", "tool",
}

// apiFamilies drives top-level dispatch and help for the typed commands.
var apiFamilies = map[string]string{
	"me":        "Who the API key is",
	"campaign":  "Campaigns and their sequences",
	"contact":   "Contacts, notes, and imports",
	"mailbox":   "Connected mailboxes and warmup",
	"inbox":     "The unified inbox",
	"analytics": "Analytics and the audit trail",
	"settings":  "Organization outreach settings",
	"webhook":   "Webhook endpoints",
	"apikey":    "API keys",
	"template":  "Reply templates",
	"crm":       "Pipelines, deals, and CRM tasks",
	"tool":      "AI agent tools (list and call the registry)",
}

// runAPIResource runs one typed command: `warmblyctl campaign start --id ...`.
func runAPIResource(ctx context.Context, family string, args []string) error {
	// `me` has no subcommands; everything else does.
	name := family
	if family != "me" {
		if len(args) == 0 {
			apiFamilyUsage(os.Stderr, family)
			return fmt.Errorf("`%s` needs a subcommand. Pick one from the list above.", family)
		}
		if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
			apiFamilyUsage(os.Stdout, family)
			return nil
		}
		name = family + " " + args[0]
		args = args[1:]
	}

	spec, ok := lookupAPISpec(name)
	if !ok {
		apiFamilyUsage(os.Stderr, family)
		return fmt.Errorf("unknown subcommand `%s`. Pick one from the list above.", name)
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { apiSpecUsage(os.Stderr, spec) }

	var id, child, data, idem *string
	if strings.Contains(spec.path, "{id}") {
		label := "resource id"
		if spec.idLabel != "" {
			label = spec.idLabel
		}
		id = fs.String("id", "", "the "+label+" (required)")
	}
	if spec.child != "" {
		child = fs.String(spec.child, "", "the "+spec.child+" id (required)")
	}
	if spec.body != bodyNone {
		data = fs.String("data", "", "JSON request body: a literal, `-` for stdin, or @file")
	}
	if spec.method != "GET" {
		idem = fs.String("idempotency-key", "", "Idempotency-Key header for a safely retryable write")
	}
	queryVals := make(map[string]*string, len(spec.query))
	for _, q := range spec.query {
		queryVals[q] = fs.String(q, "", "query parameter "+q)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	path := spec.path
	if id != nil {
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required. Example:\n  %s", specExample(spec))
		}
		path = strings.ReplaceAll(path, "{id}", url.PathEscape(strings.TrimSpace(*id)))
	}
	if child != nil {
		if strings.TrimSpace(*child) == "" {
			return fmt.Errorf("--%s is required. Example:\n  %s", spec.child, specExample(spec))
		}
		path = strings.ReplaceAll(path, "{child}", url.PathEscape(strings.TrimSpace(*child)))
	}

	var body json.RawMessage
	if data != nil {
		parsed, err := readBodyArg(*data)
		if err != nil {
			return err
		}
		body = parsed
	}
	if body == nil && spec.body == bodyRequired {
		return fmt.Errorf("--data is required: this command writes, and the body carries what to write. Example:\n  %s", specExample(spec))
	}
	if body == nil && spec.body == bodyOptional {
		body = json.RawMessage("{}")
	}

	query := url.Values{}
	for k, v := range queryVals {
		if strings.TrimSpace(*v) != "" {
			query.Set(k, strings.TrimSpace(*v))
		}
	}

	idemKey := ""
	if idem != nil {
		idemKey = *idem
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	payload, err := client.do(ctx, spec.method, path, query, bodyOrNil(body), idemKey)
	if err != nil {
		return err
	}
	return printJSON(payload)
}

// bodyOrNil keeps a nil RawMessage from being sent as the literal "null".
func bodyOrNil(body json.RawMessage) any {
	if body == nil {
		return nil
	}
	return body
}

func lookupAPISpec(name string) (apiSpec, bool) {
	for _, s := range apiSpecs {
		if s.name == name {
			return s, true
		}
	}
	return apiSpec{}, false
}

// specExample builds a copy-pasteable invocation from the spec itself, so the
// help never drifts from what the command actually accepts.
func specExample(s apiSpec) string {
	parts := []string{"warmblyctl", s.name}
	if strings.Contains(s.path, "{id}") {
		parts = append(parts, "--id "+s.idPlaceholder())
	}
	if s.child != "" {
		parts = append(parts, "--"+s.child+" <uuid>")
	}
	if s.body == bodyRequired {
		parts = append(parts, `--data '{...}'`)
	}
	return strings.Join(parts, " ")
}

func apiFamilyUsage(w *os.File, family string) {
	fmt.Fprintf(w, "%s, over the public API.\n\nUsage:\n  warmblyctl %s <subcommand> [flags]\n\nSubcommands:\n", apiFamilies[family], family)
	for _, s := range apiSpecs {
		if !strings.HasPrefix(s.name, family+" ") {
			continue
		}
		fmt.Fprintf(w, "  %-18s %s\n", strings.TrimPrefix(s.name, family+" "), s.summary)
	}
	fmt.Fprintf(w, "\nEvery command prints the API's JSON response. These need WARMBLY_API_KEY, and\nWARMBLY_API_URL when the instance is not the hosted service.\nRun `warmblyctl %s <subcommand> --help` for one command's flags.\n", family)
}

func apiSpecUsage(w *os.File, s apiSpec) {
	fmt.Fprintf(w, "%s.\n\nUsage:\n  %s\n\nCalls:\n  %s /v1%s\n", s.summary, specExample(s), s.method, s.path)
	if len(s.query) > 0 {
		fmt.Fprintf(w, "\nQuery flags: --%s\n", strings.Join(s.query, ", --"))
	}
	if s.body == bodyOptional {
		fmt.Fprint(w, "\n--data is optional; omitting it sends {}.\n")
	}
	if s.sends {
		fmt.Fprint(w, "\nThis command puts real mail on the wire.\n")
	}
}
