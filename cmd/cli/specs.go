package main

import (
	"net/http"

	"github.com/warmbly/warmbly/internal/cli/output"
)

// Every typed command in the CLI. One row per endpoint; the framework in
// resources.go turns each into a cobra command with its flags, help, request
// and table. Paths are /v1-relative and match internal/api/routes.go.

// Column helpers, so a table reads as a list of columns rather than a list of
// struct literals.
func col(header, path string) output.Column { return output.Column{Header: header, Path: path} }
func colf(header, path, format string) output.Column {
	return output.Column{Header: header, Path: path, Format: format}
}
func colt(header, path string, width int) output.Column {
	return output.Column{Header: header, Path: path, Truncate: width}
}

// Query flags every list endpoint shares.
var pageFlags = []flagSpec{
	{Name: "limit", Short: "L", Help: "How many to fetch (max 100)", Kind: flagInt, Query: true},
	{Name: "cursor", Help: "Opaque cursor from a previous page", Query: true},
}

func withPaging(extra ...flagSpec) []flagSpec {
	return append(append([]flagSpec{}, pageFlags...), extra...)
}

func resourceSpecs() []resource {
	out := []resource{}
	out = append(out, campaignSpec(), contactSpec(), suppressionSpec(), mailboxSpec(), inboxSpec())
	out = append(out, segmentSpec(), templateSpec(), automationSpec(), formSpec())
	out = append(out, dealSpec(), pipelineSpec(), taskSpec())
	out = append(out, analyticsSpec(), auditSpec(), advisorSpec())
	out = append(out, webhookSpec(), keySpec(), oauthAppSpec(), toolSpec())
	out = append(out, orgSpec(), teamSpec(), settingsSpec(), warmupRoutingSpec(), integrationSpec())
	return out
}

func campaignSpec() resource {
	campaignTable := output.Table{
		Root: "data",
		Columns: []output.Column{
			col("ID", "id"),
			colt("NAME", "name", 40),
			col("STATUS", "status"),
			col("DAILY", "daily_limit"),
			colf("CREATED", "created_at", "time"),
		},
		Empty: "No campaigns yet. Create one with `warmbly campaign create --name \"My campaign\"`.",
	}
	return resource{
		Name:    "campaign",
		Aliases: []string{"campaigns"},
		Short:   "Create, run and inspect campaigns",
		Group:   groupWork,
		Long: `Work with campaigns: the sequences, the audience, the senders, and
starting and stopping them.

Starting a campaign and sending a test both put real mail on the wire, so both
ask before they do it.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List campaigns",
				Method: http.MethodGet, Path: "/campaigns", Paginate: true,
				Example: "  $ warmbly campaign list\n  $ warmbly campaign list --status active --limit 50\n  $ warmbly campaign list --all --json",
				Flag: withPaging(
					flagSpec{Name: "query", Short: "q", Help: "Search campaign names", Query: true, Key: "q"},
					flagSpec{Name: "status", Help: "Filter by status: draft, active, paused, completed", Query: true},
					flagSpec{Name: "folder", Help: "Filter by folder", Query: true},
				),
				Table: campaignTable,
			},
			{
				Name: "view", Aliases: []string{"get", "show"}, Short: "Show one campaign",
				Method: http.MethodGet, Path: "/campaigns/{id}",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Table: output.Table{Columns: []output.Column{
					col("ID", "id"), colt("NAME", "name", 40), col("STATUS", "status"),
					col("DAILY", "daily_limit"), col("TIMEZONE", "timezone"), colf("CREATED", "created_at", "time"),
				}},
			},
			{
				Name: "overview", Short: "Status and folder counts across every campaign",
				Method: http.MethodGet, Path: "/campaigns-overview",
			},
			{
				Name: "create", Short: "Create a campaign",
				Method: http.MethodPost, Path: "/campaigns", Body: bodyRequired, Idempotent: true,
				Example: "  $ warmbly campaign create --name \"Q3 outbound\"\n  $ warmbly campaign create --name \"Q3\" --daily-limit 40 --stop-on-reply",
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Campaign name"},
					{Name: "description", Help: "What the campaign is for"},
					{Name: "daily-limit", Help: "Emails per day across the campaign", Kind: flagInt},
					{Name: "timezone", Help: "Sending timezone, for example Europe/London"},
					{Name: "stop-on-reply", Help: "Stop a contact's sequence when they reply", Kind: flagBool},
					{Name: "open-tracking", Help: "Track opens", Kind: flagBool},
					{Name: "link-tracking", Help: "Track link clicks", Kind: flagBool},
					{Name: "text-only", Help: "Send plain text only", Kind: flagBool},
				},
				Table: output.Table{Columns: []output.Column{col("ID", "id"), col("NAME", "name"), col("STATUS", "status")}},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a campaign",
				Method: http.MethodPatch, Path: "/campaigns/{id}", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Example: "  $ warmbly campaign edit CAMPAIGN_ID --daily-limit 30\n  $ warmbly campaign edit CAMPAIGN_ID --name \"Renamed\"",
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Campaign name"},
					{Name: "description", Help: "What the campaign is for"},
					{Name: "daily-limit", Help: "Emails per day across the campaign", Kind: flagInt},
					{Name: "timezone", Help: "Sending timezone"},
					{Name: "start-date", Help: "When sending may begin (RFC 3339)"},
					{Name: "end-date", Help: "When sending must stop (RFC 3339)"},
					{Name: "stop-on-reply", Help: "Stop a contact's sequence when they reply", Kind: flagBool},
				},
				Success: "Campaign updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a campaign",
				Method: http.MethodDelete, Path: "/campaigns/{id}",
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Success: "Campaign deleted.",
			},
			{
				Name: "duplicate", Short: "Copy a campaign, including its steps",
				Method: http.MethodPost, Path: "/campaigns/{id}/duplicate", Body: bodyOptional,
				Args:  []argSpec{{Name: "id", Help: "The campaign to copy"}},
				Table: output.Table{Columns: []output.Column{col("ID", "id"), col("NAME", "name"), col("STATUS", "status")}},
			},
			{
				Name: "steps", Short: "List the campaign's sequence steps",
				Method: http.MethodGet, Path: "/campaigns/{id}/steps",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), colt("SUBJECT", "subject", 44), col("WAIT", "wait_after"), col("POSITION", "position"),
				}, Empty: "This campaign has no steps yet."},
			},
			{
				Name: "add-step", Short: "Add a sequence step",
				Method: http.MethodPost, Path: "/campaigns/{id}/steps", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Example: "  $ warmbly campaign add-step CAMPAIGN_ID --subject \"Quick question\" --body-html \"<p>Hi {{first_name}}</p>\"",
				Flag: []flagSpec{
					{Name: "subject", Help: "Subject line"},
					{Name: "body-html", Help: "HTML body"},
					{Name: "body-plain", Help: "Plain text body"},
					{Name: "wait-after", Help: "Days to wait before this step", Kind: flagInt},
				},
			},
			{
				Name: "edit-step", Short: "Change a sequence step",
				Method: http.MethodPatch, Path: "/campaigns/{id}/steps/{step}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}, {Name: "step", Help: "The step's id"}},
				Flag: []flagSpec{
					{Name: "subject", Help: "Subject line"},
					{Name: "body-html", Help: "HTML body"},
					{Name: "body-plain", Help: "Plain text body"},
					{Name: "wait-after", Help: "Days to wait before this step", Kind: flagInt},
				},
				Success: "Step updated.",
			},
			{
				Name: "delete-step", Short: "Delete a sequence step",
				Method: http.MethodDelete, Path: "/campaigns/{id}/steps/{step}",
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}, {Name: "step", Help: "The step's id"}},
				Success: "Step deleted.",
			},
			{
				Name: "senders", Short: "Show the campaign's sender pool",
				Method: http.MethodGet, Path: "/campaigns/{id}/senders",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("MAILBOX", "email"), col("WEIGHT", "weight"), col("STATUS", "status"),
				}, Empty: "No explicit sender pool; the campaign uses the workspace default."},
			},
			{
				Name: "set-senders", Short: "Replace the campaign's sender pool",
				Method: http.MethodPut, Path: "/campaigns/{id}/senders", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Example: "  $ warmbly campaign set-senders CAMPAIGN_ID --input '{\"senders\":[{\"email_account_id\":\"...\",\"weight\":1}]}'",
				Success: "Sender pool replaced.",
			},
			{
				Name: "segments", Short: "List the segments feeding the campaign",
				Method: http.MethodGet, Path: "/campaigns/{id}/segments",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("NAME", "name"), col("CONTACTS", "contact_count"),
				}, Empty: "No segments are linked to this campaign."},
			},
			{
				Name: "set-segments", Short: "Replace the segments feeding the campaign",
				Method: http.MethodPut, Path: "/campaigns/{id}/segments", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Success: "Segments replaced.",
			},
			{
				Name: "attachments", Short: "List the campaign's attachments",
				Method: http.MethodGet, Path: "/campaigns/{id}/attachments",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("FILE", "filename"), col("SIZE", "size_bytes"), colf("ADDED", "created_at", "time"),
				}, Empty: "This campaign carries no attachments."},
			},
			{
				Name: "delete-attachment", Short: "Remove an attachment",
				Method: http.MethodDelete, Path: "/campaigns/{id}/attachments/{attachment}",
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}, {Name: "attachment", Help: "The attachment's id"}},
				Success: "Attachment removed.",
			},
			{
				Name: "advanced", Short: "Show the campaign's advanced settings",
				Method: http.MethodGet, Path: "/campaigns/{id}/advanced",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "set-advanced", Short: "Change the campaign's advanced settings",
				Method: http.MethodPatch, Path: "/campaigns/{id}/advanced", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Success: "Advanced settings updated.",
			},
			{
				Name: "variants", Short: "List the campaign's A/B variants",
				Method: http.MethodGet, Path: "/campaigns/{id}/ab-variants",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("NAME", "name"), colt("SUBJECT", "subject", 40), col("WEIGHT", "weight"),
				}, Empty: "This campaign has no A/B variants."},
			},
			{
				Name: "ab-analysis", Short: "Compare the campaign's A/B variants",
				Method: http.MethodGet, Path: "/campaigns/{id}/ab-analysis",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "preflight", Short: "Run the pre-send checks without sending",
				Method: http.MethodPost, Path: "/campaigns/{id}/preflight", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Long: `Run every check the campaign has to pass before it can send, and
report what would stop it. Nothing is sent.`,
			},
			{
				Name: "test", Aliases: []string{"test-email"}, Short: "Send the campaign as a test to an address you name",
				Method: http.MethodPost, Path: "/campaigns/{id}/test-email", Body: bodyRequired, Sends: true,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Example: "  $ warmbly campaign test CAMPAIGN_ID --to you@example.com",
				Flag: []flagSpec{
					{Name: "to", Help: "Where to send the test"},
					{Name: "step", Help: "Which step to send", Kind: flagInt, Key: "step_id"},
				},
				Success: "Test email sent.",
			},
			{
				Name: "start", Short: "Start the campaign",
				Method: http.MethodPost, Path: "/campaigns/{id}/start", Body: bodyOptional, Sends: true,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Success: "Campaign started.",
			},
			{
				Name: "stop", Aliases: []string{"pause"}, Short: "Stop the campaign",
				Method: http.MethodPost, Path: "/campaigns/{id}/stop", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The campaign's id"}},
				Success: "Campaign stopped.",
			},
			{
				Name: "logs", Short: "The campaign's send log",
				Method: http.MethodGet, Path: "/campaigns/{id}/logs", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("EVENT", "type"), colt("CONTACT", "contact_email", 32), colt("DETAIL", "message", 50),
				}, Empty: "Nothing in this campaign's log yet."},
			},
			{
				Name: "forms", Short: "Form performance for this campaign's recipients",
				Method: http.MethodGet, Path: "/campaigns/{id}/forms",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "verify-tracking-domain", Short: "Re-check the campaign's tracking domain",
				Method: http.MethodPost, Path: "/campaigns/{id}/tracking-domain/verify", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "estimate", Short: "Estimate how long a campaign will take to send",
				Method: http.MethodPost, Path: "/campaigns-estimate", Body: bodyRequired,
			},
		},
	}
}

func contactSpec() resource {
	contactColumns := []output.Column{
		col("ID", "id"),
		colt("EMAIL", "email", 34),
		colt("NAME", "first_name", 16),
		colt("COMPANY", "company", 24),
		col("SUBSCRIBED", "subscribed"),
		colf("ADDED", "created_at", "time"),
	}
	return resource{
		Name:    "contact",
		Aliases: []string{"contacts"},
		Short:   "Add, find and update contacts",
		Group:   groupWork,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls", "search"}, Short: "List or search contacts",
				Method: http.MethodPost, Path: "/contacts/search", Body: bodyOptional, Paginate: true,
				Long: `List contacts, optionally filtered.

The filter is a JSON body, so anything the dashboard's search can express is
available here through --input.`,
				Example: "  $ warmbly contact list\n  $ warmbly contact list --limit 100 --all\n  $ warmbly contact list --input '{\"query\":\"acme.com\"}'",
				Flag:    withPaging(),
				Table:   output.Table{Root: "data", Columns: contactColumns, Empty: "No contacts yet. Add one with `warmbly contact create --email jane@example.com`."},
			},
			{
				Name: "view", Aliases: []string{"get", "show"}, Short: "Show one contact",
				Method: http.MethodGet, Path: "/contacts/{id}",
				Args:  []argSpec{{Name: "id", Help: "The contact's id"}},
				Table: output.Table{Columns: contactColumns},
			},
			{
				Name: "lookup", Short: "Find a contact by email address",
				Method: http.MethodGet, Path: "/contacts/lookup",
				Long: `Resolve an email address to a contact.

A display-name form works too, so a raw From header can be passed straight in.`,
				Example: "  $ warmbly contact lookup --email jane@example.com",
				Flag:    []flagSpec{{Name: "email", Short: "e", Help: "The address to look up", Query: true}},
				Table:   output.Table{Root: "contact", Columns: contactColumns, Empty: "No contact with that address."},
			},
			{
				Name: "create", Aliases: []string{"add"}, Short: "Create a contact",
				Method: http.MethodPost, Path: "/contacts", Body: bodyRequired, Idempotent: true,
				Example: "  $ warmbly contact create --email jane@example.com --first-name Jane --company Acme",
				Flag: []flagSpec{
					{Name: "email", Short: "e", Help: "Email address"},
					{Name: "first-name", Help: "First name"},
					{Name: "last-name", Help: "Last name"},
					{Name: "company", Help: "Company"},
					{Name: "phone", Help: "Phone number"},
				},
				Table: output.Table{Columns: contactColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a contact",
				Method: http.MethodPatch, Path: "/contacts/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
				Flag: []flagSpec{
					{Name: "email", Short: "e", Help: "Email address"},
					{Name: "first-name", Help: "First name"},
					{Name: "last-name", Help: "Last name"},
					{Name: "company", Help: "Company"},
					{Name: "phone", Help: "Phone number"},
					{Name: "subscribed", Help: "Whether the contact may receive campaign mail", Kind: flagBool},
				},
				Success: "Contact updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a contact",
				Method: http.MethodDelete, Path: "/contacts/{id}",
				Args:    []argSpec{{Name: "id", Help: "The contact's id"}},
				Success: "Contact deleted.",
			},
			{
				Name: "timeline", Short: "Everything that happened to a contact, newest first",
				Method: http.MethodGet, Path: "/contacts/{id}/timeline", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("EVENT", "type"), colt("DETAIL", "description", 60),
				}, Empty: "Nothing has happened to this contact yet."},
			},
			{
				Name: "emails", Short: "Emails sent to a contact",
				Method: http.MethodGet, Path: "/contacts/{id}/emails", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), colt("SUBJECT", "subject", 46), col("STATUS", "status"),
				}, Empty: "No mail has gone to this contact."},
			},
			{
				Name: "campaigns", Short: "Campaigns a contact is in",
				Method: http.MethodGet, Path: "/contacts/{id}/campaigns",
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "campaign_id"), colt("CAMPAIGN", "name", 40), col("STATUS", "status"), col("SENT", "sent"),
				}, Empty: "This contact is not in any campaign."},
			},
			{
				Name: "notes", Short: "List a contact's notes",
				Method: http.MethodGet, Path: "/contacts/{id}/notes",
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), colt("NOTE", "content", 60), colf("ADDED", "created_at", "time"),
				}, Empty: "No notes on this contact."},
			},
			{
				Name: "add-note", Short: "Add a note to a contact",
				Method: http.MethodPost, Path: "/contacts/{id}/notes", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The contact's id"}},
				Flag:    []flagSpec{{Name: "content", Short: "m", Help: "The note"}},
				Success: "Note added.",
			},
			{
				Name: "activities", Short: "A contact's recorded activities",
				Method: http.MethodGet, Path: "/contacts/{id}/activities", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("TYPE", "activity_type"),
				}, Empty: "No activities recorded."},
			},
			{
				Name: "custom-fields", Short: "The custom field keys in use",
				Method: http.MethodGet, Path: "/contacts/custom-fields",
			},
			{
				Name: "import-preview", Short: "Preview a bulk import without writing anything",
				Method: http.MethodPost, Path: "/contacts/import/preview", Body: bodyRequired,
			},
			{
				Name: "import", Short: "Commit a previewed bulk import",
				Method: http.MethodPost, Path: "/contacts/import/commit", Body: bodyRequired, Idempotent: true,
			},
			{
				Name: "export", Short: "Export contacts",
				Method: http.MethodPost, Path: "/contacts/export", Body: bodyOptional,
			},
			{
				Name: "verify", Short: "Queue address verification for contacts",
				Method: http.MethodPost, Path: "/contacts/verification", Body: bodyRequired,
			},
			{
				Name: "verification-status", Short: "How address verification is going",
				Method: http.MethodGet, Path: "/contacts/verification",
			},
			{
				Name: "research", Short: "Run AI research on a contact",
				Method: http.MethodPost, Path: "/contacts/{id}/research", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
			},
			{
				Name: "research-result", Short: "Show a contact's research",
				Method: http.MethodGet, Path: "/contacts/{id}/research",
				Args: []argSpec{{Name: "id", Help: "The contact's id"}},
			},
		},
	}
}

func suppressionSpec() resource {
	return resource{
		Name:    "suppression",
		Aliases: []string{"suppressions"},
		Short:   "Addresses and domains that get no campaign mail",
		Group:   groupData,
		Long: `The workspace suppression list.

Anything on it is skipped by every campaign, which is what keeps an
unsubscribe or a complaint from being undone by the next import.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List suppressed addresses and domains",
				Method: http.MethodGet, Path: "/suppressions", Paginate: true,
				Flag: withPaging(flagSpec{Name: "query", Short: "q", Help: "Search the list", Query: true, Key: "q"}),
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("VALUE", "value"), col("KIND", "kind"), col("REASON", "reason"), colf("ADDED", "created_at", "time"),
				}, Empty: "Nothing is suppressed."},
			},
			{
				Name: "add", Short: "Suppress an address or a domain",
				Method: http.MethodPost, Path: "/suppressions", Body: bodyRequired,
				Example: "  $ warmbly suppression add --input '{\"values\":[\"jane@example.com\"],\"reason\":\"manual\"}'",
			},
			{
				Name: "remove", Aliases: []string{"rm"}, Short: "Lift a suppression",
				Method: http.MethodDelete, Path: "/suppressions/{id}",
				Args:    []argSpec{{Name: "id", Help: "The suppression entry's id"}},
				Success: "Suppression lifted.",
			},
		},
	}
}

func mailboxSpec() resource {
	mailboxColumns := []output.Column{
		col("ID", "id"),
		colt("MAILBOX", "email", 34),
		col("PROVIDER", "provider"),
		col("STATUS", "status"),
		col("DAILY", "campaign_limit"),
		colf("SYNCED", "last_synced_at", "time"),
	}
	return resource{
		Name:    "mailbox",
		Aliases: []string{"mailboxes", "email", "emails"},
		Short:   "Connected sending mailboxes, their health and their warmup",
		Group:   groupWork,
		Long: `Work with the mailboxes this workspace sends from.

Connecting a new mailbox needs a browser (OAuth consent or a credential form),
so that stays in the dashboard: ` + "`warmbly browse mailboxes`" + ` opens it.
Everything after connection is here.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List connected mailboxes",
				Method: http.MethodGet, Path: "/emails", Paginate: true,
				Flag:  withPaging(flagSpec{Name: "query", Short: "q", Help: "Search addresses", Query: true, Key: "q"}),
				Table: output.Table{Root: "data", Columns: mailboxColumns, Empty: "No mailboxes connected. Connect one with `warmbly browse mailboxes`."},
			},
			{
				Name: "view", Aliases: []string{"get", "show"}, Short: "Show one mailbox",
				Method: http.MethodGet, Path: "/emails/{id}",
				Args:  []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Table: output.Table{Columns: mailboxColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a mailbox's settings",
				Method: http.MethodPatch, Path: "/emails/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Long: `Change a mailbox's sending settings.

The daily cap is the safety control that matters most: 50 a day is the product
default and the top of the normal band for cold outreach. Above 100 the
dashboard warns, and it should.`,
				Example: "  $ warmbly mailbox edit MAILBOX_ID --daily-limit 40\n  $ warmbly mailbox edit MAILBOX_ID --min-wait 900",
				Flag: []flagSpec{
					{Name: "name", Help: "Display name on outgoing mail"},
					{Name: "daily-limit", Help: "Campaign emails per day from this mailbox", Kind: flagInt, Key: "campaign_limit"},
					{Name: "min-wait", Help: "Minimum seconds between sends", Kind: flagInt, Key: "min_wait_time"},
					{Name: "reply-to", Help: "Reply-To address"},
					{Name: "signature", Help: "Plain text signature", Key: "signature_plain"},
					{Name: "timezone", Help: "The mailbox's timezone"},
				},
				Success: "Mailbox updated.",
			},
			{
				Name: "remove", Aliases: []string{"rm", "delete"}, Short: "Disconnect a mailbox",
				Method: http.MethodDelete, Path: "/emails/{id}",
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Mailbox disconnected.",
			},
			{
				Name: "check", Aliases: []string{"auth-check"}, Short: "Show the mailbox's SPF, DKIM and DMARC",
				Method: http.MethodGet, Path: "/emails/{id}/auth-check",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Table: output.Table{Columns: []output.Column{
					col("STATE", "auth_state"), col("SPF", "auth_spf"), col("DKIM", "auth_dkim"),
					col("DMARC", "auth_dmarc"), col("POLICY", "auth_dmarc_policy"), colf("CHECKED", "auth_checked_at", "time"),
				}},
			},
			{
				Name: "recheck", Short: "Re-run the authentication check now",
				Method: http.MethodPost, Path: "/emails/{id}/auth-check", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "sync", Short: "The mailbox's sync state and backfill progress",
				Method: http.MethodGet, Path: "/emails/{id}/sync",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "behavior", Short: "The mailbox's human-sending ranges",
				Method: http.MethodGet, Path: "/emails/{id}/behavior",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "set-behavior", Short: "Change the mailbox's sending behaviour",
				Method: http.MethodPut, Path: "/emails/{id}/behavior", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Sending behaviour updated.",
			},
			{
				Name: "behavior-plan", Short: "What the behaviour settings mean in practice",
				Method: http.MethodGet, Path: "/emails/{id}/behavior/plan",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "verify", Short: "Verify an email address without sending to it",
				Method: http.MethodPost, Path: "/emails/verify", Body: bodyRequired,
				Flag: []flagSpec{{Name: "email", Short: "e", Help: "The address to verify"}},
			},
			{
				Name: "send", Short: "Send one email from this mailbox",
				Method: http.MethodPost, Path: "/emails/{id}/send", Body: bodyRequired, Sends: true, Idempotent: true,
				Args:    []argSpec{{Name: "id", Help: "The mailbox to send from"}},
				Example: "  $ warmbly mailbox send MAILBOX_ID --to jane@example.com --subject Hello --body \"Hi Jane\"",
				Flag: []flagSpec{
					{Name: "to", Help: "Recipient address"},
					{Name: "subject", Help: "Subject line"},
					{Name: "body", Help: "Message body", Key: "body_html"},
					{Name: "cc", Help: "CC addresses", Kind: flagStrings},
					{Name: "bcc", Help: "BCC addresses", Kind: flagStrings},
				},
				Success: "Email sent.",
			},
			{
				Name: "hold", Short: "Hold the mailbox out of campaign sending",
				Method: http.MethodPost, Path: "/emails/{id}/hold", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Mailbox held out of campaign sending.",
			},
			{
				Name: "release", Short: "Put a held mailbox back into campaign sending",
				Method: http.MethodPost, Path: "/emails/{id}/release", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Mailbox released back into campaign sending.",
			},
			{
				Name: "warmup-start", Short: "Start warming the mailbox",
				Method: http.MethodPost, Path: "/emails/{id}/warmup/start", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Long: `Start warmup for this mailbox.

Warmup ramps gradually from the product default of ten a day; it is not a
switch that produces volume today, and it should keep running once campaigns
begin rather than being stopped the moment the mailbox looks ready.`,
				Success: "Warmup started.",
			},
			{
				Name: "warmup-pause", Short: "Pause warmup",
				Method: http.MethodPost, Path: "/emails/{id}/warmup/pause", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Warmup paused.",
			},
			{
				Name: "warmup-resume", Short: "Resume warmup",
				Method: http.MethodPost, Path: "/emails/{id}/warmup/resume", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Warmup resumed.",
			},
			{
				Name: "warmup-stop", Short: "Stop warmup",
				Method: http.MethodPost, Path: "/emails/{id}/warmup/stop", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Success: "Warmup stopped.",
			},
			{
				Name: "warmup-status", Short: "The mailbox's warmup pool standing",
				Method: http.MethodGet, Path: "/emails/{id}/warmup/ban-status",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "warmup-appeal", Short: "Appeal a warmup pool block",
				Method: http.MethodPost, Path: "/emails/{id}/warmup/appeal", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "tracking", Short: "The mailbox's tracking domain",
				Method: http.MethodGet, Path: "/emails/{id}/track",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "set-tracking", Short: "Set the mailbox's tracking domain",
				Method: http.MethodPatch, Path: "/emails/{id}/track", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The mailbox's id"}},
				Flag:    []flagSpec{{Name: "domain", Help: "The tracking domain", Key: "tracking_domain"}},
				Success: "Tracking domain set.",
			},
			{
				Name: "verify-tracking", Short: "Check the tracking domain's DNS",
				Method: http.MethodPost, Path: "/emails/{id}/track/verify", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "tags", Short: "Change tags across mailboxes",
				Method: http.MethodPatch, Path: "/emails/tags", Body: bodyRequired,
				Success: "Tags updated.",
			},
		},
	}
}

func inboxSpec() resource {
	messageColumns := []output.Column{
		col("ID", "id"),
		colt("FROM", "from_addr", 28),
		colt("SUBJECT", "subject", 44),
		col("SEEN", "seen"),
		colf("WHEN", "internal_date", "time"),
	}
	return resource{
		Name:    "inbox",
		Aliases: []string{"unibox"},
		Short:   "Read and reply to mail across every mailbox",
		Group:   groupWork,
		Long: `The unified inbox: every connected mailbox in one stream.

Replying and composing put real mail on the wire, so both ask before they do.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List inbox messages",
				Method: http.MethodGet, Path: "/unibox", Paginate: true,
				Example: "  $ warmbly inbox list\n  $ warmbly inbox list --unseen\n  $ warmbly inbox list --from acme.com --limit 50",
				Flag: withPaging(
					flagSpec{Name: "address", Help: "Conversations with this address, either direction", Query: true},
					flagSpec{Name: "direction", Help: "sent or received", Query: true},
					flagSpec{Name: "folder", Help: "inbox, sent, drafts, archive, spam or trash", Query: true},
					flagSpec{Name: "from", Help: "Filter by sender", Query: true},
					flagSpec{Name: "subject", Help: "Filter by subject", Query: true},
					flagSpec{Name: "unseen", Help: "Only unread messages", Kind: flagBool, Query: true},
					flagSpec{Name: "awaiting-reply", Help: "Only threads waiting on a reply", Kind: flagBool, Query: true, Key: "awaiting_reply"},
					flagSpec{Name: "since", Help: "Only messages after this time (RFC 3339)", Query: true},
					flagSpec{Name: "until", Help: "Only messages before this time (RFC 3339)", Query: true},
				),
				Table: output.Table{Root: "data", Columns: messageColumns, Empty: "Nothing in the inbox."},
			},
			{
				Name: "view", Aliases: []string{"get", "show"}, Short: "Show one message",
				Method: http.MethodGet, Path: "/unibox/{id}",
				Args: []argSpec{{Name: "id", Help: "The message's id"}},
			},
			{
				Name: "count", Short: "The unread count",
				Method: http.MethodGet, Path: "/unibox/count",
			},
			{
				Name: "overview", Short: "Per-mailbox and per-tag inbox rollup",
				Method: http.MethodGet, Path: "/unibox/overview",
			},
			{
				Name: "thread", Short: "One conversation, oldest first",
				Method: http.MethodGet, Path: "/unibox/thread", Paginate: true,
				Long: `Read one conversation.

--thread-id is required and is the thread_id on any message from ` + "`warmbly inbox list`" + `.
Without --mailbox the thread is read across every mailbox in the workspace,
which is the unified view.`,
				Example: "  $ warmbly inbox thread --thread-id THREAD_ID",
				Flag: withPaging(
					flagSpec{Name: "thread-id", Help: "The thread's id (required)", Query: true, Key: "thread_id"},
					flagSpec{Name: "mailbox", Help: "Narrow to one mailbox", Query: true, Key: "email_id"},
				),
				Table: output.Table{Root: "data", Columns: messageColumns, Empty: "No messages in that thread."},
			},
			{
				Name: "read", Short: "Mark messages read",
				Method: http.MethodPatch, Path: "/unibox/seen", Body: bodyRequired,
				Example: "  $ warmbly inbox read --input '{\"ids\":[\"...\"],\"seen\":true}'",
				Success: "Marked.",
			},
			{
				Name: "reply", Short: "Reply in a thread",
				Method: http.MethodPost, Path: "/unibox/reply", Body: bodyRequired, Sends: true, Idempotent: true,
				Example: "  $ warmbly inbox reply --input '{\"email_id\":\"...\",\"body_html\":\"<p>Thanks</p>\"}'",
				Success: "Reply sent.",
			},
			{
				Name: "compose", Short: "Send a new email",
				Method: http.MethodPost, Path: "/unibox/compose", Body: bodyRequired, Sends: true, Idempotent: true,
				Flag: []flagSpec{
					{Name: "from", Help: "The mailbox to send from", Key: "email_account_id"},
					{Name: "to", Help: "Recipient addresses", Kind: flagStrings},
					{Name: "subject", Help: "Subject line"},
					{Name: "body", Help: "Message body", Key: "body_html"},
				},
				Success: "Email sent.",
			},
			{
				Name: "drafts", Short: "List your saved drafts",
				Method: http.MethodGet, Path: "/unibox/drafts",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), colt("SUBJECT", "subject", 44), colf("SAVED", "updated_at", "time"),
				}, Empty: "No drafts saved."},
			},
			{
				Name: "delete-draft", Short: "Delete a saved draft",
				Method: http.MethodDelete, Path: "/unibox/drafts/{id}",
				Args:    []argSpec{{Name: "id", Help: "The draft's id"}},
				Success: "Draft deleted.",
			},
			{
				Name: "agent-drafts", Short: "Replies the inbox agent drafted for review",
				Method: http.MethodGet, Path: "/unibox/agent-drafts",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), colt("SUBJECT", "subject", 40), colt("TO", "to_addr", 28), colf("DRAFTED", "created_at", "time"),
				}, Empty: "The inbox agent has nothing waiting."},
			},
			{
				Name: "approve-draft", Short: "Approve an agent draft, which sends it",
				Method: http.MethodPost, Path: "/unibox/agent-drafts/{id}/approve", Body: bodyOptional, Sends: true,
				Args:    []argSpec{{Name: "id", Help: "The draft's id"}},
				Success: "Draft approved and sent.",
			},
			{
				Name: "discard-draft", Short: "Discard an agent draft",
				Method: http.MethodPost, Path: "/unibox/agent-drafts/{id}/discard", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The draft's id"}},
				Success: "Draft discarded.",
			},
			{
				Name: "scheduled", Short: "List scheduled sends",
				Method: http.MethodGet, Path: "/unibox/scheduled",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("TASK", "task_id"), colt("SUBJECT", "subject", 40), colf("SENDS", "scheduled_for", "date"),
				}, Empty: "Nothing is scheduled."},
			},
			{
				Name: "cancel-scheduled", Short: "Cancel a scheduled send",
				Method: http.MethodDelete, Path: "/unibox/scheduled/{task}",
				Args:    []argSpec{{Name: "task", Help: "The scheduled task's id"}},
				Success: "Scheduled send cancelled.",
			},
			{
				Name: "snooze", Short: "Snooze a thread until later",
				Method: http.MethodPost, Path: "/unibox/snooze", Body: bodyRequired,
				Success: "Thread snoozed.",
			},
			{
				Name: "unsnooze", Short: "Bring a snoozed thread back now",
				Method: http.MethodDelete, Path: "/unibox/snooze", Body: bodyRequired,
				Success: "Thread unsnoozed.",
			},
			{
				Name: "snoozes", Short: "List snoozed threads",
				Method: http.MethodGet, Path: "/unibox/snoozes",
			},
			{
				Name: "labels", Short: "A thread's labels",
				Method: http.MethodGet, Path: "/unibox/thread/labels",
				Flag: []flagSpec{{Name: "thread-id", Help: "The thread to read labels for (required)", Query: true, Key: "thread_id"}},
			},
			{
				Name: "set-labels", Short: "Replace a thread's labels",
				Method: http.MethodPut, Path: "/unibox/thread/labels", Body: bodyRequired,
				Success: "Labels updated.",
			},
		},
	}
}

func segmentSpec() resource {
	segmentColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 34), col("MATCH", "match"),
		col("CONTACTS", "contact_count"), colf("CREATED", "created_at", "time"),
	}
	return resource{
		Name:    "segment",
		Aliases: []string{"segments"},
		Short:   "Live contact audiences built from conditions",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List segments",
				Method: http.MethodGet, Path: "/segments",
				Table: output.Table{Root: "data", Columns: segmentColumns, Empty: "No segments yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one segment",
				Method: http.MethodGet, Path: "/segments/{id}",
				Args:  []argSpec{{Name: "id", Help: "The segment's id"}},
				Table: output.Table{Columns: segmentColumns},
			},
			{
				Name: "create", Short: "Create a segment",
				Method: http.MethodPost, Path: "/segments", Body: bodyRequired,
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Segment name"},
					{Name: "description", Help: "What the segment is for"},
					{Name: "match", Help: "all or any"},
				},
				Table: output.Table{Columns: segmentColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a segment",
				Method: http.MethodPatch, Path: "/segments/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The segment's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Segment name"},
					{Name: "description", Help: "What the segment is for"},
					{Name: "match", Help: "all or any"},
				},
				Success: "Segment updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a segment",
				Method: http.MethodDelete, Path: "/segments/{id}",
				Args:    []argSpec{{Name: "id", Help: "The segment's id"}},
				Success: "Segment deleted.",
			},
			{
				Name: "fields", Short: "Every field a condition can match on",
				Method: http.MethodGet, Path: "/segments/fields",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("FIELD", "field"), col("LABEL", "label"), col("GROUP", "group"), col("KIND", "kind"),
				}, Empty: "No segment fields reported."},
			},
			{
				Name: "preview", Short: "Count what a set of conditions would match",
				Method: http.MethodPost, Path: "/segments/preview", Body: bodyRequired,
			},
			{
				Name: "members", Short: "Add or remove explicit members",
				Method: http.MethodPost, Path: "/segments/{id}/members", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The segment's id"}},
				Success: "Membership updated.",
			},
			{
				Name: "overrides", Short: "Contacts forced in or out of the segment",
				Method: http.MethodGet, Path: "/segments/{id}/overrides",
				Args: []argSpec{{Name: "id", Help: "The segment's id"}},
			},
			{
				Name: "add-to-campaign", Short: "Enrol the segment's contacts in a campaign",
				Method: http.MethodPost, Path: "/segments/{id}/add-to-campaign", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The segment's id"}},
				Flag: []flagSpec{{Name: "campaign", Help: "The campaign to add them to", Key: "campaign_id"}},
			},
		},
	}
}

func templateSpec() resource {
	templateColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 30), colt("SUBJECT", "subject", 40), colf("UPDATED", "updated_at", "time"),
	}
	return resource{
		Name:    "template",
		Aliases: []string{"templates"},
		Short:   "Reply templates",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List templates",
				Method: http.MethodGet, Path: "/templates",
				Table: output.Table{Root: "data", Columns: templateColumns, Empty: "No templates yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one template",
				Method: http.MethodGet, Path: "/templates/{id}",
				Args:  []argSpec{{Name: "id", Help: "The template's id"}},
				Table: output.Table{Columns: templateColumns},
			},
			{
				Name: "create", Short: "Create a template",
				Method: http.MethodPost, Path: "/templates", Body: bodyRequired,
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Template name"},
					{Name: "subject", Help: "Subject line"},
					{Name: "body-html", Help: "HTML body"},
					{Name: "body-plain", Help: "Plain text body"},
				},
				Table: output.Table{Columns: templateColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a template",
				Method: http.MethodPatch, Path: "/templates/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The template's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Template name"},
					{Name: "subject", Help: "Subject line"},
					{Name: "body-html", Help: "HTML body"},
					{Name: "body-plain", Help: "Plain text body"},
				},
				Success: "Template updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a template",
				Method: http.MethodDelete, Path: "/templates/{id}",
				Args:    []argSpec{{Name: "id", Help: "The template's id"}},
				Success: "Template deleted.",
			},
			{
				Name: "duplicate", Short: "Copy a template",
				Method: http.MethodPost, Path: "/templates/{id}/duplicate", Body: bodyOptional,
				Args:  []argSpec{{Name: "id", Help: "The template's id"}},
				Table: output.Table{Columns: templateColumns},
			},
			{
				Name: "render", Short: "Render a template with variables filled in",
				Method: http.MethodPost, Path: "/templates/{id}/render", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The template's id"}},
			},
			{
				Name: "score", Short: "Score a draft for deliverability and tone",
				Method: http.MethodPost, Path: "/templates/score", Body: bodyRequired,
			},
			{
				Name: "reorder", Short: "Change the order templates appear in",
				Method: http.MethodPatch, Path: "/templates/reorder", Body: bodyRequired,
				Success: "Templates reordered.",
			},
		},
	}
}

func automationSpec() resource {
	automationColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 34), col("STATUS", "status"), colf("UPDATED", "updated_at", "time"),
	}
	return resource{
		Name:    "automation",
		Aliases: []string{"automations"},
		Short:   "Event-driven automations",
		Group:   groupWork,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List automations",
				Method: http.MethodGet, Path: "/automations",
				Table: output.Table{Root: "data", Columns: automationColumns, Empty: "No automations yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one automation",
				Method: http.MethodGet, Path: "/automations/{id}",
				Args:  []argSpec{{Name: "id", Help: "The automation's id"}},
				Table: output.Table{Columns: automationColumns},
			},
			{
				Name: "create", Short: "Create an automation",
				Method: http.MethodPost, Path: "/automations", Body: bodyRequired,
				Flag:  []flagSpec{{Name: "name", Short: "n", Help: "Automation name"}},
				Table: output.Table{Columns: automationColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change an automation",
				Method: http.MethodPatch, Path: "/automations/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The automation's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Automation name"},
					{Name: "status", Help: "active or paused"},
				},
				Success: "Automation updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete an automation",
				Method: http.MethodDelete, Path: "/automations/{id}",
				Args:    []argSpec{{Name: "id", Help: "The automation's id"}},
				Success: "Automation deleted.",
			},
			{
				Name: "test", Short: "Run an automation once against test input",
				Method: http.MethodPost, Path: "/automations/{id}/test", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The automation's id"}},
			},
			{
				Name: "runs", Short: "Recent runs of an automation",
				Method: http.MethodGet, Path: "/automations/{id}/runs", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The automation's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("STATUS", "status"), colf("STARTED", "created_at", "time"), colt("DETAIL", "error", 40),
				}, Empty: "This automation has not run yet."},
			},
		},
	}
}

func formSpec() resource {
	formColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 30), col("STATUS", "status"),
		col("VIEWS", "views_count"), col("SUBMISSIONS", "submissions_count"),
	}
	return resource{
		Name:    "form",
		Aliases: []string{"forms"},
		Short:   "Lead capture forms",
		Group:   groupWork,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List forms",
				Method: http.MethodGet, Path: "/forms",
				Table: output.Table{Root: "data", Columns: formColumns, Empty: "No forms yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one form",
				Method: http.MethodGet, Path: "/forms/{id}",
				Args:  []argSpec{{Name: "id", Help: "The form's id"}},
				Table: output.Table{Columns: formColumns},
			},
			{
				Name: "create", Short: "Create a form",
				Method: http.MethodPost, Path: "/forms", Body: bodyRequired,
				Flag:  []flagSpec{{Name: "name", Short: "n", Help: "Form name"}},
				Table: output.Table{Columns: formColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a form",
				Method: http.MethodPatch, Path: "/forms/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The form's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Form name"},
					{Name: "status", Help: "draft or published"},
					{Name: "redirect-url", Help: "Where to send a visitor after submitting"},
				},
				Success: "Form updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a form",
				Method: http.MethodDelete, Path: "/forms/{id}",
				Args:    []argSpec{{Name: "id", Help: "The form's id"}},
				Success: "Form deleted.",
			},
			{
				Name: "submissions", Short: "A form's submissions",
				Method: http.MethodGet, Path: "/forms/{id}/submissions", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The form's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), colt("EMAIL", "email", 34), colf("WHEN", "created_at", "time"),
				}, Empty: "No submissions yet."},
			},
			{
				Name: "stats", Short: "A form's view and completion numbers",
				Method: http.MethodGet, Path: "/forms/{id}/stats",
				Args: []argSpec{{Name: "id", Help: "The form's id"}},
			},
			{
				Name: "config", Short: "The workspace's form configuration",
				Method: http.MethodGet, Path: "/forms/config",
			},
			{
				Name: "domain", Short: "The domain forms are served from",
				Method: http.MethodGet, Path: "/forms/domain",
			},
			{
				Name: "set-domain", Short: "Set the domain forms are served from",
				Method: http.MethodPut, Path: "/forms/domain", Body: bodyRequired,
				Flag:    []flagSpec{{Name: "domain", Help: "The custom forms domain"}},
				Success: "Forms domain set.",
			},
			{
				Name: "verify-domain", Short: "Check the forms domain's DNS",
				Method: http.MethodPost, Path: "/forms/domain/verify", Body: bodyOptional,
			},
		},
	}
}

func dealSpec() resource {
	dealColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 32), col("STATUS", "status"),
		col("VALUE", "value"), col("CURRENCY", "currency"), colf("CLOSES", "expected_close_date", "date"),
	}
	return resource{
		Name:    "deal",
		Aliases: []string{"deals"},
		Short:   "CRM deals",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List deals",
				Method: http.MethodGet, Path: "/crm/deals", Paginate: true,
				Flag:  withPaging(),
				Table: output.Table{Root: "data", Columns: dealColumns, Empty: "No deals yet."},
			},
			{
				Name: "search", Short: "Search deals with a filter body",
				Method: http.MethodPost, Path: "/crm/deals/search", Body: bodyOptional, Paginate: true,
				Flag:  withPaging(),
				Table: output.Table{Root: "data", Columns: dealColumns, Empty: "Nothing matched."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one deal",
				Method: http.MethodGet, Path: "/crm/deals/{id}",
				Args:  []argSpec{{Name: "id", Help: "The deal's id"}},
				Table: output.Table{Columns: dealColumns},
			},
			{
				Name: "create", Short: "Create a deal",
				Method: http.MethodPost, Path: "/crm/deals", Body: bodyRequired,
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Deal name"},
					{Name: "pipeline", Help: "The pipeline it belongs to", Key: "pipeline_id"},
					{Name: "stage", Help: "The stage it starts in", Key: "stage_id"},
					{Name: "contact", Help: "The contact it is with", Key: "contact_id"},
					{Name: "value", Help: "Deal value", Kind: flagInt},
					{Name: "currency", Help: "Currency code"},
				},
				Table: output.Table{Columns: dealColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a deal",
				Method: http.MethodPatch, Path: "/crm/deals/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The deal's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Deal name"},
					{Name: "stage", Help: "Move it to this stage", Key: "stage_id"},
					{Name: "status", Help: "open, won or lost"},
					{Name: "value", Help: "Deal value", Kind: flagInt},
				},
				Success: "Deal updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a deal",
				Method: http.MethodDelete, Path: "/crm/deals/{id}",
				Args:    []argSpec{{Name: "id", Help: "The deal's id"}},
				Success: "Deal deleted.",
			},
			{
				Name: "summary", Short: "Deal totals by stage and status",
				Method: http.MethodPost, Path: "/crm/deals/summary", Body: bodyOptional,
			},
		},
	}
}

func pipelineSpec() resource {
	return resource{
		Name:    "pipeline",
		Aliases: []string{"pipelines"},
		Short:   "CRM pipelines and their stages",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List pipelines with their stages",
				Method: http.MethodGet, Path: "/crm/pipelines",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("NAME", "name"), col("POSITION", "position"),
				}, Empty: "No pipelines yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one pipeline",
				Method: http.MethodGet, Path: "/crm/pipelines/{id}",
				Args: []argSpec{{Name: "id", Help: "The pipeline's id"}},
			},
			{
				Name: "create", Short: "Create a pipeline",
				Method: http.MethodPost, Path: "/crm/pipelines", Body: bodyRequired,
				Flag: []flagSpec{{Name: "name", Short: "n", Help: "Pipeline name"}},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Rename a pipeline",
				Method: http.MethodPatch, Path: "/crm/pipelines/{id}", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The pipeline's id"}},
				Flag:    []flagSpec{{Name: "name", Short: "n", Help: "Pipeline name"}},
				Success: "Pipeline updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a pipeline",
				Method: http.MethodDelete, Path: "/crm/pipelines/{id}",
				Args:    []argSpec{{Name: "id", Help: "The pipeline's id"}},
				Success: "Pipeline deleted.",
			},
			{
				Name: "add-stage", Short: "Add a stage to a pipeline",
				Method: http.MethodPost, Path: "/crm/pipelines/{id}/stages", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The pipeline's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Stage name"},
					{Name: "color", Help: "Stage colour"},
				},
			},
			{
				Name: "edit-stage", Short: "Change a stage",
				Method: http.MethodPatch, Path: "/crm/pipelines/{id}/stages/{stage}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The pipeline's id"}, {Name: "stage", Help: "The stage's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Stage name"},
					{Name: "color", Help: "Stage colour"},
				},
				Success: "Stage updated.",
			},
			{
				Name: "delete-stage", Short: "Delete a stage",
				Method: http.MethodDelete, Path: "/crm/pipelines/{id}/stages/{stage}",
				Args:    []argSpec{{Name: "id", Help: "The pipeline's id"}, {Name: "stage", Help: "The stage's id"}},
				Success: "Stage deleted.",
			},
		},
	}
}

func taskSpec() resource {
	taskColumns := []output.Column{
		col("ID", "id"), colt("TITLE", "title", 40), col("STATUS", "status"), colf("DUE", "due_at", "date"),
	}
	return resource{
		Name:    "task",
		Aliases: []string{"tasks"},
		Short:   "CRM tasks and follow-ups",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List tasks",
				Method: http.MethodGet, Path: "/crm/tasks", Paginate: true,
				Flag:  withPaging(),
				Table: output.Table{Root: "data", Columns: taskColumns, Empty: "No tasks."},
			},
			{
				Name: "search", Short: "Search tasks with a filter body",
				Method: http.MethodPost, Path: "/crm/tasks/search", Body: bodyOptional, Paginate: true,
				Flag:  withPaging(),
				Table: output.Table{Root: "data", Columns: taskColumns, Empty: "Nothing matched."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one task",
				Method: http.MethodGet, Path: "/crm/tasks/{id}",
				Args:  []argSpec{{Name: "id", Help: "The task's id"}},
				Table: output.Table{Columns: taskColumns},
			},
			{
				Name: "create", Short: "Create a task",
				Method: http.MethodPost, Path: "/crm/tasks", Body: bodyRequired,
				Flag: []flagSpec{
					{Name: "title", Short: "t", Help: "What needs doing"},
					{Name: "contact", Help: "The contact it is about", Key: "contact_id"},
					{Name: "due", Help: "When it is due (RFC 3339)", Key: "due_at"},
				},
				Table: output.Table{Columns: taskColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a task",
				Method: http.MethodPatch, Path: "/crm/tasks/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The task's id"}},
				Flag: []flagSpec{
					{Name: "title", Short: "t", Help: "What needs doing"},
					{Name: "status", Help: "open or done"},
					{Name: "due", Help: "When it is due (RFC 3339)", Key: "due_at"},
				},
				Success: "Task updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a task",
				Method: http.MethodDelete, Path: "/crm/tasks/{id}",
				Args:    []argSpec{{Name: "id", Help: "The task's id"}},
				Success: "Task deleted.",
			},
			{
				Name: "summary", Short: "Task counts by status",
				Method: http.MethodPost, Path: "/crm/tasks/summary", Body: bodyOptional,
			},
			{
				Name: "types", Short: "The task types in use",
				Method: http.MethodGet, Path: "/crm/task-types",
			},
		},
	}
}

func analyticsSpec() resource {
	return resource{
		Name:    "analytics",
		Aliases: []string{"stats"},
		Short:   "The numbers: sends, opens, replies, deliverability, warmup",
		Group:   groupData,
		Endpoints: []endpoint{
			{Name: "dashboard", Short: "The headline numbers", Method: http.MethodGet, Path: "/analytics/dashboard"},
			{Name: "deliverability", Short: "Bounces, complaints and placement", Method: http.MethodGet, Path: "/analytics/deliverability"},
			{
				Name: "warmup", Short: "Warmup analytics over a date range",
				Method: http.MethodGet, Path: "/analytics/warmup",
				Example: "  $ warmbly analytics warmup --from 2026-01-01 --to 2026-01-31",
				Flag: []flagSpec{
					{Name: "from", Help: "Start of the range, YYYY-MM-DD", Query: true},
					{Name: "to", Help: "End of the range, YYYY-MM-DD", Query: true},
					{Name: "mailbox", Help: "Only this mailbox", Query: true, Key: "email_id"},
				},
			},
			{
				Name: "mailboxes", Aliases: []string{"accounts"}, Short: "Per-mailbox analytics",
				Method: http.MethodGet, Path: "/analytics/accounts",
				Table: output.Table{Root: "data", Columns: []output.Column{
					colt("MAILBOX", "email", 32), col("SENT", "sent"), col("OPENED", "opened"),
					col("REPLIED", "replied"), col("BOUNCED", "bounced"),
				}, Empty: "No mailbox analytics yet."},
			},
			{
				Name: "mailbox", Aliases: []string{"account"}, Short: "One mailbox's analytics",
				Method: http.MethodGet, Path: "/analytics/accounts/{id}",
				Args: []argSpec{{Name: "id", Help: "The mailbox's id"}},
			},
			{
				Name: "campaign", Short: "One campaign's analytics",
				Method: http.MethodGet, Path: "/analytics/campaigns/{id}",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "campaign-daily", Short: "One campaign's daily series",
				Method: http.MethodGet, Path: "/analytics/campaigns/{id}/daily",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "campaign-hourly", Short: "One campaign's hourly series",
				Method: http.MethodGet, Path: "/analytics/campaigns/{id}/hourly",
				Args: []argSpec{{Name: "id", Help: "The campaign's id"}},
			},
			{
				Name: "compare", Short: "Compare campaigns side by side over a date range",
				Method: http.MethodGet, Path: "/analytics/campaigns/compare",
				Example: "  $ warmbly analytics compare --ids ID_A,ID_B --from 2026-01-01 --to 2026-01-31",
				Flag: []flagSpec{
					{Name: "ids", Help: "Campaign ids to compare (required)", Kind: flagStrings, Query: true},
					{Name: "from", Help: "Start of the range, YYYY-MM-DD (required)", Query: true},
					{Name: "to", Help: "End of the range, YYYY-MM-DD (required)", Query: true},
				},
			},
			{Name: "usage", Short: "API and plan usage", Method: http.MethodGet, Path: "/analytics/usage"},
		},
	}
}

func auditSpec() resource {
	return resource{
		Name:  "audit",
		Short: "The workspace's audit trail",
		Group: groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls", "log"}, Short: "List audit entries, newest first",
				Method: http.MethodGet, Path: "/audit-logs", Paginate: true,
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("ACTION", "action"), col("ENTITY", "entity_type"),
					colt("ACTOR", "actor_email", 30), colt("IP", "ip_address", 18),
				}, Empty: "Nothing in the audit log yet."},
			},
		},
	}
}

func advisorSpec() resource {
	return resource{
		Name:  "advisor",
		Short: "Warmbly's recommendations for this workspace",
		Group: groupData,
		Endpoints: []endpoint{
			{Name: "summary", Short: "What the advisor thinks overall", Method: http.MethodGet, Path: "/advisor/summary"},
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List open recommendations",
				Method: http.MethodGet, Path: "/advisor/recommendations",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("SEVERITY", "severity"), colt("TITLE", "title", 50), col("STATUS", "status"),
				}, Empty: "Nothing to advise on. That is the good outcome."},
			},
			{Name: "settings", Short: "How the advisor is configured", Method: http.MethodGet, Path: "/advisor/settings"},
			{Name: "refresh", Short: "Recompute the recommendations now", Method: http.MethodPost, Path: "/advisor/refresh", Body: bodyOptional},
			{
				Name: "apply", Short: "Apply a recommendation",
				Method: http.MethodPost, Path: "/advisor/recommendations/{id}/apply", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The recommendation's id"}},
				Success: "Recommendation applied.",
			},
			{
				Name: "dismiss", Short: "Dismiss a recommendation",
				Method: http.MethodPost, Path: "/advisor/recommendations/{id}/dismiss", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The recommendation's id"}},
				Success: "Recommendation dismissed.",
			},
			{
				Name: "snooze", Short: "Snooze a recommendation",
				Method: http.MethodPost, Path: "/advisor/recommendations/{id}/snooze", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The recommendation's id"}},
				Success: "Recommendation snoozed.",
			},
			{
				Name: "undo", Short: "Undo an applied recommendation",
				Method: http.MethodPost, Path: "/advisor/recommendations/{id}/undo", Body: bodyOptional,
				Args:    []argSpec{{Name: "id", Help: "The recommendation's id"}},
				Success: "Recommendation undone.",
			},
		},
	}
}

func webhookSpec() resource {
	webhookColumns := []output.Column{
		col("ID", "id"), colt("URL", "url", 44), col("ENABLED", "enabled"),
		col("FAILURES", "consecutive_failures"), colf("LAST OK", "last_success_at", "time"),
	}
	return resource{
		Name:    "webhook",
		Aliases: []string{"webhooks"},
		Short:   "Webhook endpoints and their deliveries",
		Group:   groupDevelop,
		Long: `Manage the HTTPS endpoints Warmbly posts events to.

Every delivery is HMAC signed with the endpoint's secret, so rotating a secret
means updating the receiver at the same time.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List webhook endpoints",
				Method: http.MethodGet, Path: "/webhooks",
				Table: output.Table{Root: "data", Columns: webhookColumns, Empty: "No webhook endpoints yet."},
			},
			{
				Name: "create", Short: "Create a webhook endpoint",
				Method: http.MethodPost, Path: "/webhooks", Body: bodyRequired,
				Example: "  $ warmbly webhook create --url https://example.com/hooks/warmbly --events EMAIL_REPLIED,EMAIL_BOUNCED",
				Flag: []flagSpec{
					{Name: "url", Help: "Where to POST events"},
					{Name: "description", Help: "What this endpoint is for"},
					{Name: "events", Help: "Event types to subscribe to", Kind: flagStrings, Key: "event_types"},
				},
				Table: output.Table{Columns: webhookColumns},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a webhook endpoint",
				Method: http.MethodPatch, Path: "/webhooks/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The endpoint's id"}},
				Flag: []flagSpec{
					{Name: "url", Help: "Where to POST events"},
					{Name: "description", Help: "What this endpoint is for"},
					{Name: "events", Help: "Event types to subscribe to", Kind: flagStrings, Key: "event_types"},
					{Name: "enabled", Help: "Whether it receives events", Kind: flagBool},
				},
				Success: "Webhook updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a webhook endpoint",
				Method: http.MethodDelete, Path: "/webhooks/{id}",
				Args:    []argSpec{{Name: "id", Help: "The endpoint's id"}},
				Success: "Webhook deleted.",
			},
			{
				Name: "verify", Short: "Send a verification ping to the endpoint",
				Method: http.MethodPost, Path: "/webhooks/{id}/verify", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The endpoint's id"}},
			},
			{
				Name: "rotate-secret", Short: "Rotate the endpoint's signing secret",
				Method: http.MethodPost, Path: "/webhooks/{id}/rotate-secret", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The endpoint's id"}},
				Long: `Rotate the HMAC secret this endpoint's deliveries are signed with.

The new secret is in the response and nowhere else. Update the receiver before
the next event fires, or its signature check will fail.`,
			},
			{
				Name: "deliveries", Short: "Recent deliveries across endpoints",
				Method: http.MethodGet, Path: "/webhooks/deliveries", Paginate: true,
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("EVENT", "event_type"), col("STATUS", "response_status"), colt("ERROR", "error", 40),
				}, Empty: "Nothing has been delivered yet."},
			},
			{
				Name: "redeliver", Short: "Send one delivery again",
				Method: http.MethodPost, Path: "/webhooks/deliveries/{delivery}/redeliver", Body: bodyOptional,
				Args:    []argSpec{{Name: "delivery", Help: "The delivery's id"}},
				Success: "Redelivery queued.",
			},
			{
				Name: "event-types", Short: "Every event type a webhook can subscribe to",
				Method: http.MethodGet, Path: "/webhooks/event-types",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("TYPE", "type"), col("CATEGORY", "category"), colt("DESCRIPTION", "description", 60),
				}, Empty: "No event types reported."},
			},
			{
				Name: "drops", Short: "Events dropped because the endpoint was throttled",
				Method: http.MethodGet, Path: "/webhooks/throttle-drops",
			},
		},
	}
}

func keySpec() resource {
	keyColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 34), col("PREFIX", "key_prefix"),
		col("STATUS", "status"), colf("LAST USED", "last_used_at", "time"),
	}
	return resource{
		Name:    "key",
		Aliases: []string{"keys", "api-key", "api-keys"},
		Short:   "API keys for this workspace",
		Group:   groupDevelop,
		Long: `Manage the workspace's API keys.

The key this CLI is signed in with is one of these, named for the machine that
created it. A key's secret exists only in the create response.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List API keys",
				Method: http.MethodGet, Path: "/api-keys", Paginate: true,
				Flag:  withPaging(),
				Table: output.Table{Root: "data", Columns: keyColumns, Empty: "No API keys yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one API key",
				Method: http.MethodGet, Path: "/api-keys/{id}",
				Args:  []argSpec{{Name: "id", Help: "The key's id"}},
				Table: output.Table{Columns: keyColumns},
			},
			{
				Name: "create", Short: "Create an API key",
				Method: http.MethodPost, Path: "/api-keys", Body: bodyRequired,
				Long: `Create an API key.

The secret is in this response and is never retrievable again. Store it before
you close the terminal.`,
				Example: "  $ warmbly key create --name \"CI deploy\" --permissions 8388607",
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "What the key is for"},
					{Name: "description", Help: "Longer note"},
					{Name: "permissions", Help: "Scope bitmask; see `warmbly key permissions`", Kind: flagInt},
					{Name: "rate-limit", Help: "Requests per minute for this key", Kind: flagInt, Key: "rate_limit_per_minute"},
					{Name: "allowed-ips", Help: "Restrict the key to these IPs or CIDRs", Kind: flagStrings, Key: "allowed_ips"},
				},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a key's name, scopes or restrictions",
				Method: http.MethodPatch, Path: "/api-keys/{id}", Body: bodyRequired,
				Args: []argSpec{{Name: "id", Help: "The key's id"}},
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "What the key is for"},
					{Name: "description", Help: "Longer note"},
					{Name: "permissions", Help: "Scope bitmask", Kind: flagInt},
					{Name: "rate-limit", Help: "Requests per minute for this key", Kind: flagInt, Key: "rate_limit_per_minute"},
				},
				Success: "API key updated.",
			},
			{
				Name: "revoke", Aliases: []string{"rm", "delete"}, Short: "Revoke an API key",
				Method: http.MethodDelete, Path: "/api-keys/{id}",
				Args:    []argSpec{{Name: "id", Help: "The key's id"}},
				Success: "API key revoked.",
			},
			{
				Name: "permissions", Aliases: []string{"scopes"}, Short: "Every grantable scope and its bit value",
				Method: http.MethodGet, Path: "/api-keys/permissions",
			},
			{
				Name: "logs", Short: "One key's request log",
				Method: http.MethodGet, Path: "/api-keys/{id}/logs", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The key's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("METHOD", "method"), colt("ENDPOINT", "endpoint", 40),
					col("STATUS", "response_code"), col("MS", "response_time_ms"),
				}, Empty: "This key has not been used."},
			},
			{
				Name: "analytics", Short: "One key's usage over time",
				Method: http.MethodGet, Path: "/api-keys/{id}/analytics",
				Args: []argSpec{{Name: "id", Help: "The key's id"}},
				Flag: []flagSpec{{Name: "interval", Help: "hour or day", Query: true}},
			},
			{
				Name: "usage", Short: "API key usage across the workspace",
				Method: http.MethodGet, Path: "/api-keys/usage/summary",
			},
		},
	}
}

func oauthAppSpec() resource {
	appColumns := []output.Column{
		col("ID", "id"), colt("NAME", "name", 30), col("CLIENT", "client_id"), colf("CREATED", "created_at", "time"),
	}
	return resource{
		Name:    "oauth-app",
		Aliases: []string{"oauth-apps"},
		Short:   "OAuth applications you publish",
		Group:   groupDevelop,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List OAuth applications",
				Method: http.MethodGet, Path: "/oauth/applications",
				Table: output.Table{Root: "data", Columns: appColumns, Empty: "No OAuth applications yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one application",
				Method: http.MethodGet, Path: "/oauth/applications/{id}",
				Args:  []argSpec{{Name: "id", Help: "The application's id"}},
				Table: output.Table{Columns: appColumns},
			},
			{
				Name: "create", Short: "Create an OAuth application",
				Method: http.MethodPost, Path: "/oauth/applications", Body: bodyRequired,
				Flag: []flagSpec{
					{Name: "name", Short: "n", Help: "Application name"},
					{Name: "redirect-uris", Help: "Allowed redirect URIs", Kind: flagStrings, Key: "redirect_uris"},
				},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change an application",
				Method: http.MethodPatch, Path: "/oauth/applications/{id}", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The application's id"}},
				Success: "Application updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete an application",
				Method: http.MethodDelete, Path: "/oauth/applications/{id}",
				Args:    []argSpec{{Name: "id", Help: "The application's id"}},
				Success: "Application deleted.",
			},
			{
				Name: "rotate-secret", Short: "Rotate the application's client secret",
				Method: http.MethodPost, Path: "/oauth/applications/{id}/rotate-secret", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The application's id"}},
			},
		},
	}
}

func toolSpec() resource {
	return resource{
		Name:    "tool",
		Aliases: []string{"tools"},
		Short:   "The AI tool registry, callable over REST",
		Group:   groupDevelop,
		Long: `The same tool registry the dashboard agent and MCP use, exposed as
plain REST for function-calling agents that do not speak MCP.

Everything a tool can do is bounded by the signed-in key's scopes.`,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List the tools this key may call",
				Method: http.MethodGet, Path: "/ai/tools",
				Flag: []flagSpec{{Name: "format", Help: "openai emits function-calling manifests", Query: true}},
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("NAME", "name"), colt("DESCRIPTION", "description", 60),
				}, Empty: "No tools available to this key."},
			},
			{
				Name: "call", Short: "Run one tool",
				Method: http.MethodPost, Path: "/ai/tools/{name}/call", Body: bodyOptional,
				Args:    []argSpec{{Name: "name", Help: "The tool's name, not a uuid"}},
				Example: "  $ warmbly tool call list_campaigns\n  $ warmbly tool call search_contacts -f query=acme.com",
			},
		},
	}
}

// orgSpec is deliberately one command. Every /organization/* route is JWT
// only: members, roles, invitations, exports and the danger zone all depend on
// a human-bound session and refuse an API key, which is the only credential
// this CLI holds. Shipping commands that can never succeed would be worse than
// not shipping them, so what is left is what GET /me answers, and the rest is
// a link to the dashboard.
func orgSpec() resource {
	return resource{
		Name:    "org",
		Aliases: []string{"organization", "workspace"},
		Short:   "Which workspace you are signed in to",
		Group:   groupData,
		Long: `Show the workspace this credential belongs to.

Members, roles, invitations, billing and workspace exports are session-only on
the API and cannot be driven with a key. ` + "`warmbly browse settings`" + ` opens
them in the dashboard.`,
		Endpoints: []endpoint{
			{
				Name: "view", Aliases: []string{"current", "me"}, Short: "Show the workspace and who you are in it",
				Method: http.MethodGet, Path: "/me",
				Table: output.Table{Columns: []output.Column{
					col("USER", "email"),
					col("WORKSPACE", "organization_name"),
					col("WORKSPACE ID", "organization_id"),
					col("AUTH", "auth_type"),
				}},
			},
		},
	}
}

func teamSpec() resource {
	return resource{
		Name:    "team",
		Aliases: []string{"teams"},
		Short:   "Named groups of members, for CRM ownership and routing",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List teams",
				Method: http.MethodGet, Path: "/teams",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("NAME", "name"), col("MEMBERS", "member_count"),
				}, Empty: "No teams yet."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one team",
				Method: http.MethodGet, Path: "/teams/{id}",
				Args: []argSpec{{Name: "id", Help: "The team's id"}},
			},
			{
				Name: "create", Short: "Create a team",
				Method: http.MethodPost, Path: "/teams", Body: bodyRequired,
				Flag: []flagSpec{{Name: "name", Short: "n", Help: "Team name"}},
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Rename a team",
				Method: http.MethodPatch, Path: "/teams/{id}", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The team's id"}},
				Flag:    []flagSpec{{Name: "name", Short: "n", Help: "Team name"}},
				Success: "Team updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a team",
				Method: http.MethodDelete, Path: "/teams/{id}",
				Args:    []argSpec{{Name: "id", Help: "The team's id"}},
				Success: "Team deleted.",
			},
			{
				Name: "add-member", Short: "Add a member to a team",
				Method: http.MethodPost, Path: "/teams/{id}/members", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The team's id"}},
				Flag:    []flagSpec{{Name: "user", Help: "The member's user id", Key: "user_id"}},
				Success: "Member added.",
			},
			{
				Name: "remove-member", Short: "Remove a member from a team",
				Method: http.MethodDelete, Path: "/teams/{id}/members/{user}",
				Args:    []argSpec{{Name: "id", Help: "The team's id"}, {Name: "user", Help: "The member's user id"}},
				Success: "Member removed.",
			},
		},
	}
}

func settingsSpec() resource {
	return resource{
		Name:  "settings",
		Short: "Workspace-wide sending and suppression settings",
		Group: groupData,
		Endpoints: []endpoint{
			{
				Name: "view", Aliases: []string{"get", "outreach"}, Short: "Show the outreach settings",
				Method: http.MethodGet, Path: "/outreach/settings",
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change the outreach settings",
				Method: http.MethodPatch, Path: "/outreach/settings", Body: bodyRequired,
				Long: `Change the workspace's outreach settings.

These apply across every campaign, so they are the right place for a policy
decision (auto-suppress on bounce, honour unsubscribes globally) and the wrong
place for a per-campaign one.`,
				Success: "Outreach settings updated.",
			},
		},
	}
}

func warmupRoutingSpec() resource {
	return resource{
		Name:    "warmup-routing",
		Aliases: []string{"routing"},
		Short:   "Rules deciding which mailboxes warm with which",
		Group:   groupData,
		Endpoints: []endpoint{
			{
				Name: "list", Aliases: []string{"ls"}, Short: "List routing rules",
				Method: http.MethodGet, Path: "/warmup/routing",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("NAME", "name"), col("ENABLED", "enabled"),
				}, Empty: "No warmup routing rules; the default pool policy applies."},
			},
			{
				Name: "create", Short: "Create a routing rule",
				Method: http.MethodPost, Path: "/warmup/routing", Body: bodyRequired,
			},
			{
				Name: "edit", Aliases: []string{"update"}, Short: "Change a routing rule",
				Method: http.MethodPatch, Path: "/warmup/routing/{id}", Body: bodyRequired,
				Args:    []argSpec{{Name: "id", Help: "The rule's id"}},
				Success: "Routing rule updated.",
			},
			{
				Name: "delete", Aliases: []string{"rm"}, Short: "Delete a routing rule",
				Method: http.MethodDelete, Path: "/warmup/routing/{id}",
				Args:    []argSpec{{Name: "id", Help: "The rule's id"}},
				Success: "Routing rule deleted.",
			},
		},
	}
}

func integrationSpec() resource {
	return resource{
		Name:    "integration",
		Aliases: []string{"integrations"},
		Short:   "Third-party connections",
		Group:   groupDevelop,
		Long: `Manage connections to third-party tools.

Connecting one usually needs a browser (OAuth consent), so creating a
connection stays in the dashboard. Everything after that is here.`,
		Endpoints: []endpoint{
			{
				Name: "catalog", Short: "Every integration this instance offers",
				Method: http.MethodGet, Path: "/integrations/catalog",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("KEY", "key"), col("NAME", "name"), col("CATEGORY", "category"),
				}, Empty: "No integrations available."},
			},
			{
				Name: "list", Aliases: []string{"ls", "connections"}, Short: "List this workspace's connections",
				Method: http.MethodGet, Path: "/integrations/connections",
				Table: output.Table{Root: "data", Columns: []output.Column{
					col("ID", "id"), col("PROVIDER", "provider"), col("STATUS", "status"), colf("CONNECTED", "created_at", "time"),
				}, Empty: "Nothing connected."},
			},
			{
				Name: "view", Aliases: []string{"get"}, Short: "Show one connection",
				Method: http.MethodGet, Path: "/integrations/connections/{id}",
				Args: []argSpec{{Name: "id", Help: "The connection's id"}},
			},
			{
				Name: "test", Short: "Check a connection still works",
				Method: http.MethodPost, Path: "/integrations/connections/{id}/test", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The connection's id"}},
			},
			{
				Name: "runs", Short: "A connection's recent runs",
				Method: http.MethodGet, Path: "/integrations/connections/{id}/runs", Paginate: true,
				Args: []argSpec{{Name: "id", Help: "The connection's id"}},
				Flag: withPaging(),
				Table: output.Table{Root: "data", Columns: []output.Column{
					colf("WHEN", "created_at", "time"), col("STATUS", "status"), colt("DETAIL", "error", 44),
				}, Empty: "This connection has not run yet."},
			},
			{
				Name: "push", Short: "Push data through a connection now",
				Method: http.MethodPost, Path: "/integrations/connections/{id}/push", Body: bodyOptional,
				Args: []argSpec{{Name: "id", Help: "The connection's id"}},
			},
			{
				Name: "disconnect", Aliases: []string{"rm"}, Short: "Disconnect an integration",
				Method: http.MethodDelete, Path: "/integrations/connections/{id}",
				Args:    []argSpec{{Name: "id", Help: "The connection's id"}},
				Success: "Integration disconnected.",
			},
		},
	}
}
