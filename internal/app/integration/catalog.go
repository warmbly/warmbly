// Package integration owns the third-party integrations surface: catalog
// metadata, OAuth connect flows, per-provider connect/disconnect, event-driven
// actions, and inbound webhook handling for Calendly, Cal.com and Clay.
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
		string(models.WebhookEventMeetingBooked),
		string(models.WebhookEventMeetingCanceled),
	}
	notifyEvents = []string{
		string(models.WebhookEventCampaignReplyReceived),
		string(models.WebhookEventMeetingBooked),
		string(models.WebhookEventMeetingRescheduled),
		string(models.WebhookEventMeetingCanceled),
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
			Tagline:    "Run your CRM on HubSpot: deals, tasks, owners and every email, in sync both ways.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://docs.warmbly.com/guides/hubspot/",
			Highlights: []string{
				"Sends and replies logged as real HubSpot emails",
				"HubSpot deals, tasks, notes and owners right inside Warmbly",
				"Campaigns stop when a deal opens or a contact becomes a customer",
				"Import leads from any HubSpot list",
			},
			Events:       crmEvents,
			ActionTypes:  []string{string(models.IntegrationActionHubSpotUpsert)},
			SupportsPush: true,
		},
		{
			Provider:   models.IntegrationSalesforce,
			Name:       "Salesforce",
			Tagline:    "Two-way sync with Leads, Contacts and the activity timeline.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://docs.warmbly.com/guides/salesforce/",
			Highlights: []string{
				"Production, sandbox or My Domain, connected in one click",
				"Sends, replies and meetings logged as Tasks on the Lead or Contact",
				"Lead Status writeback and do-not-email kept in step both ways",
				"Import from list views and Salesforce Campaigns, kept in sync",
				"Owner, status and open deals on every contact and inbox thread",
			},
			Events:       crmEvents,
			ActionTypes:  []string{string(models.IntegrationActionSalesforceUpsert)},
			SupportsPush: true,
		},
		{
			Provider:   models.IntegrationPipedrive,
			Name:       "Pipedrive",
			Tagline:    "Run your CRM on Pipedrive: persons, deals and activities, two-way.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: string(models.IntegrationAuthOAuth),
			DocsURL:    "https://docs.warmbly.com/guides/pipedrive/",
			Highlights: []string{
				"Deals, activities, notes and pipelines are Pipedrive's, edited from Warmbly",
				"Sends, replies and meetings logged as activities on the person and deal",
				"Interested replies label the person, open a deal or add a lead",
				"Campaigns stop when a deal opens or a person is labeled a customer",
				"Import people from any saved Pipedrive filter",
			},
			Events:       crmEvents,
			ActionTypes:  []string{string(models.IntegrationActionPipedriveUpsert)},
			SupportsPush: true,
		},
		{
			Provider:     models.IntegrationClose,
			Name:         "Close",
			Tagline:      "Leads, contacts, and inbox activity for Close.",
			Category:     models.IntegrationCategoryCRM,
			AuthMethod:   string(models.IntegrationAuthAPIKey),
			DocsURL:      "https://developer.close.com",
			Highlights:   []string{"Paste your Close API key (no OAuth app available)", "Upsert a lead on reply or on demand"},
			Events:       crmEvents,
			ActionTypes:  []string{string(models.IntegrationActionCloseUpsert)},
			SupportsPush: true,
		},

		// Automation --------------------------------------------------------
		{
			Provider:    models.IntegrationZapier,
			Name:        "Zapier",
			Tagline:     "Triggers and actions across 8,000+ apps.",
			Category:    models.IntegrationCategoryAutomation,
			AuthMethod:  string(models.IntegrationAuthAPIKey),
			DocsURL:     "https://zapier.com/apps",
			Highlights:  []string{"Authenticate Zapier with a scoped Warmbly API key", "Fan events to a Zap webhook URL"},
			Events:      notifyEvents,
			ActionTypes: []string{string(models.IntegrationActionGenericWebhookPing)},
		},
		{
			Provider:    models.IntegrationMake,
			Name:        "Make",
			Tagline:     "Visual automation scenarios.",
			Category:    models.IntegrationCategoryAutomation,
			AuthMethod:  string(models.IntegrationAuthAPIKey),
			DocsURL:     "https://www.make.com/en/integrations",
			Highlights:  []string{"Authenticate Make with a scoped Warmbly API key", "Fan events to a Make webhook URL"},
			Events:      notifyEvents,
			ActionTypes: []string{string(models.IntegrationActionGenericWebhookPing)},
		},
		{
			Provider:    models.IntegrationN8N,
			Name:        "n8n",
			Tagline:     "Self-hosted automation workflows.",
			Category:    models.IntegrationCategoryAutomation,
			AuthMethod:  string(models.IntegrationAuthAPIKey),
			DocsURL:     "https://docs.n8n.io",
			Highlights:  []string{"Authenticate n8n with a scoped Warmbly API key", "Fan events to an n8n webhook URL"},
			Events:      notifyEvents,
			ActionTypes: []string{string(models.IntegrationActionGenericWebhookPing)},
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
				"Ask the Warmbly assistant and contact support from Slack",
			},
			Events:      notifyEvents,
			ActionTypes: []string{string(models.IntegrationActionSlackNotify)},
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
			ActionTypes: []string{string(models.IntegrationActionDiscordNotify)},
		},

		// Meetings ----------------------------------------------------------
		{
			Provider:    models.IntegrationCalendly,
			Name:        "Calendly",
			Tagline:     "Booked, rescheduled, and canceled calls land on the contact, live.",
			Category:    models.IntegrationCategoryMeetings,
			AuthMethod:  string(models.IntegrationAuthWebhook),
			DocsURL:     "https://developer.calendly.com/api-docs/",
			WebhookHint: "Paste the URL we mint into Calendly's webhook subscription (invitee.created + invitee.canceled).",
			Highlights: []string{
				"We mint an inbound URL for you",
				"Meetings appear on the contact timeline and the Meetings page in real time",
				"Trigger automations when a call is booked (Slack ping, CRM upsert)",
				"Add a 'Book a call' button anywhere a contact is in view",
			},
		},
		{
			Provider:    models.IntegrationCalCom,
			Name:        "Cal.com",
			Tagline:     "Open-source booking, same live meeting tracking.",
			Category:    models.IntegrationCategoryMeetings,
			AuthMethod:  string(models.IntegrationAuthWebhook),
			DocsURL:     "https://cal.com/docs/core-features/webhooks",
			WebhookHint: "Paste the URL we mint into Cal.com's webhook (BOOKING_CREATED / RESCHEDULED / CANCELLED).",
			Highlights: []string{
				"We mint an inbound URL for you",
				"Meetings appear on the contact timeline and the Meetings page in real time",
				"Trigger automations when a call is booked (Slack ping, CRM upsert)",
				"Add a 'Book a call' button anywhere a contact is in view",
			},
		},

		// Data ----------------------------------------------------------------
		{
			Provider:   models.IntegrationCompanyEnrich,
			Name:       "CompanyEnrich",
			Tagline:    "Research companies and people, and fill missing contact details.",
			Category:   models.IntegrationCategoryData,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://docs.warmbly.com/guides/companyenrich/",
			Highlights: []string{
				"Connect with your own CompanyEnrich API key",
				"Search companies and people, find lookalikes and explore workforce data",
				"Enrich by domain, company properties or a batch of domains",
				"Fill missing details on existing contacts without changing their email or subscription status",
			},
		},
		{
			Provider:   models.IntegrationApollo,
			Name:       "Apollo",
			Tagline:    "Import Apollo lists and enrich your contacts from Apollo's database.",
			Category:   models.IntegrationCategoryData,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://docs.warmbly.com/guides/apollo/",
			Highlights: []string{
				"Connect with your own Apollo API key",
				"Import any saved Apollo list, once or kept in sync every hour",
				"Only verified addresses by default; suppressed and unsubscribed people stay out",
				"Enrich selected contacts with title, company, LinkedIn and location",
			},
		},
		{
			Provider:   models.IntegrationClay,
			Name:       "Clay",
			Tagline:    "Send enriched rows from a Clay table into Warmbly, and replies back to Clay.",
			Category:   models.IntegrationCategoryData,
			AuthMethod: string(models.IntegrationAuthWebhook),
			DocsURL:    "https://docs.warmbly.com/guides/clay/",
			Highlights: []string{
				"We mint an inbound URL for Clay's HTTP API column",
				"Rows become contacts, added to a campaign, category or segment",
				"Every column you send is kept as a custom field",
				"Replies, opens, bounces and meetings sent back to a Clay table",
			},
			Events:      ClayEvents,
			ActionTypes: []string{string(models.IntegrationActionClaySendRow)},
		},

		// NOTE: Google Sheets is intentionally NOT a catalog integration. The
		// google_sheets OAuth connection still exists (it powers the on-demand
		// Lead Sync feature under Contacts), but it is no longer surfaced as an
		// integration tile and has no event-driven append-row automation. See
		// internal/app/leadsync.
		// Verification --------------------------------------------------------
		{
			Provider:   models.IntegrationMillionVerifier,
			Name:       "MillionVerifier",
			Tagline:    "Pay-as-you-go address verification for every contact you import.",
			Category:   models.IntegrationCategoryVerification,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://www.millionverifier.com/",
			Highlights: []string{
				"Every new contact is checked automatically; nothing to run",
				"One credit per address, bought from MillionVerifier as you go",
				"Replaces the built-in check while credits last, falls back when they run out",
			},
		},
		{
			Provider:   models.IntegrationCleanMyList,
			Name:       "CleanMyList",
			Tagline:    "Verify contact addresses with your CleanMyList account.",
			Category:   models.IntegrationCategoryVerification,
			AuthMethod: string(models.IntegrationAuthAPIKey),
			DocsURL:    "https://www.cleanmylist.io/developers",
			Highlights: []string{
				"Checks new contacts and re-verifies existing addresses",
				"Uses your CleanMyList plan allowance first, then credits",
				"Falls back to the built-in check when the service is unavailable",
			},
		},
	}
}

// ProviderLabel is the catalog's display name for a provider, for copy a member
// reads. Falls back to the identifier for anything not in the catalog.
func ProviderLabel(p models.IntegrationProvider) string {
	for _, e := range Catalog() {
		if e.Provider == p {
			return e.Name
		}
	}
	return string(p)
}
