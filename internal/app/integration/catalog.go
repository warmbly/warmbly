// Package integration owns the third-party integrations surface: catalog
// metadata, OAuth connect flows, per-provider connect/disconnect, event-driven
// actions, and inbound webhook handling for Calendly + Cal.com.
//
// Per-provider files (oauth.go, slack.go, hubspot.go, discord.go,
// google_sheets.go, calendly.go) each handle the provider-specific request /
// response shapes. The shared service.go ties them to the connections repo so
// the dashboard reads them uniformly.
package integration

import "github.com/warmbly/warmbly/internal/models"

// Reply / bounce / meeting are the most actionable events for outbound teams,
// so they're offered as triggers on the relevant providers.
var (
	crmEvents = []string{
		string(models.WebhookEventCampaignReplyReceived),
		string(models.WebhookEventCampaignEmailBounced),
		string(models.WebhookEventCampaignUnsubscribed),
	}
	notifyEvents = []string{
		string(models.WebhookEventCampaignReplyReceived),
		string(models.WebhookEventCampaignEmailBounced),
		string(models.WebhookEventWarmupHealthChanged),
		string(models.WebhookEventDeliverabilityComplaint),
	}
)

// Catalog returns the static metadata for every integration the dashboard
// renders. Order is the catalog order users see. Per-connection Configured /
// Scopes are filled in by the service from the OAuth manager.
func Catalog() []models.IntegrationCatalogEntry {
	return []models.IntegrationCatalogEntry{
		// CRM ---------------------------------------------------------------
		{
			Provider:   models.IntegrationHubSpot,
			Name:       "HubSpot",
			Tagline:    "Push positive replies and new leads straight into your CRM.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://developers.hubspot.com/docs/api/overview",
			Highlights: []string{
				"One-click OAuth — no API keys to copy",
				"Create or update a HubSpot contact when a prospect replies",
				"Log the reply as a note on the contact timeline",
			},
			Events: crmEvents,
		},
		{
			Provider:   models.IntegrationSalesforce,
			Name:       "Salesforce",
			Tagline:    "Sync leads, contacts, and email activity to Salesforce.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://developer.salesforce.com/docs",
			Highlights: []string{"OAuth connect", "Lead + contact sync on reply"},
			Events:     crmEvents,
		},
		{
			Provider:   models.IntegrationPipedrive,
			Name:       "Pipedrive",
			Tagline:    "Persons, deals, and an activity timeline that stays in sync.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://developers.pipedrive.com",
			Highlights: []string{"OAuth connect", "Upsert a person when a prospect replies"},
			Events:     crmEvents,
		},
		{
			Provider:   models.IntegrationClose,
			Name:       "Close",
			Tagline:    "Leads, contacts, and inbox activity for Close.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://developer.close.com",
			Highlights: []string{"Paste your Close API key (no OAuth app available)"},
		},

		// Automation --------------------------------------------------------
		{
			Provider:   models.IntegrationZapier,
			Name:       "Zapier",
			Tagline:    "Triggers and actions across 8,000+ apps.",
			Category:   models.IntegrationCategoryAutomation,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://zapier.com/apps",
			Highlights: []string{"Authenticate Zapier with a scoped Warmbly API key"},
		},
		{
			Provider:   models.IntegrationMake,
			Name:       "Make",
			Tagline:    "Visual automation scenarios.",
			Category:   models.IntegrationCategoryAutomation,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://www.make.com/en/integrations",
			Highlights: []string{"Authenticate Make with a scoped Warmbly API key"},
		},
		{
			Provider:   models.IntegrationN8N,
			Name:       "n8n",
			Tagline:    "Self-hosted automation workflows.",
			Category:   models.IntegrationCategoryAutomation,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://docs.n8n.io",
			Highlights: []string{"Authenticate n8n with a scoped Warmbly API key"},
		},

		// Notifications -----------------------------------------------------
		{
			Provider:   models.IntegrationSlack,
			Name:       "Slack",
			Tagline:    "Real-time alerts for positive replies, bounces, and deliverability.",
			Category:   models.IntegrationCategoryNotifications,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://api.slack.com",
			Highlights: []string{
				"One-click OAuth into your workspace",
				"Ping a channel the moment a prospect replies",
				"Warn the team when warmup health or deliverability dips",
			},
			Events: notifyEvents,
		},
		{
			Provider:    models.IntegrationDiscord,
			Name:        "Discord",
			Tagline:     "Webhook-based notifications to a server channel.",
			Category:    models.IntegrationCategoryNotifications,
			AuthMethod:  string(models.IntegrationAuthWebhook),
			DocsURL:     "https://discord.com/developers/docs/resources/webhook",
			WebhookHint: "Paste a Discord channel webhook URL.",
			Highlights:  []string{"Paste a channel webhook URL", "Ping on reply / bounce / warmup health"},
			Events:      notifyEvents,
		},

		// Meetings ----------------------------------------------------------
		{
			Provider:    models.IntegrationCalendly,
			Name:        "Calendly",
			Tagline:     "Attribute booked meetings to the campaign that surfaced the lead.",
			Category:    models.IntegrationCategoryMeetings,
			AuthMethod:  string(models.IntegrationAuthWebhook),
			DocsURL:     "https://developer.calendly.com/api-docs/",
			WebhookHint: "Calendly POSTs invitee.created to the URL we mint.",
			Highlights:  []string{"We mint an inbound URL for you", "Booked meetings credit the originating campaign"},
		},
		{
			Provider:    models.IntegrationCalCom,
			Name:        "Cal.com",
			Tagline:     "Same attribution path, open-source booking edition.",
			Category:    models.IntegrationCategoryMeetings,
			AuthMethod:  string(models.IntegrationAuthWebhook),
			DocsURL:     "https://cal.com/docs/core-features/webhooks",
			WebhookHint: "Cal.com POSTs BOOKING_CREATED to the URL we mint.",
			Highlights:  []string{"We mint an inbound URL for you", "Booked meetings credit the originating campaign"},
		},

		// NOTE: Google Sheets is intentionally NOT a catalog integration. The
		// google_sheets OAuth connection still exists (it powers the on-demand
		// Lead Sync feature under Contacts), but it is no longer surfaced as an
		// integration tile and has no event-driven append-row automation. See
		// internal/app/leadsync.
	}
}
