// Package integration owns the third-party integrations surface: catalog
// metadata, per-provider connect/disconnect, and inbound webhook handling
// for Calendly + Cal.com.
//
// Per-provider files (calendly.go, google_sheets.go) each handle the
// provider-specific request/response shape. The shared service.go ties
// them to the connections repo so the dashboard reads them uniformly.
package integration

import "github.com/warmbly/warmbly/internal/models"

// Catalog returns the static metadata for every integration the dashboard
// renders. Order is the catalog order users see.
func Catalog() []models.IntegrationCatalogEntry {
	return []models.IntegrationCatalogEntry{
		// CRM
		{
			Provider:   models.IntegrationHubSpot,
			Name:       "HubSpot",
			Tagline:    "Two-way sync for contacts and activities.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: "oauth",
			DocsURL:    "https://developers.hubspot.com/docs/api/overview",
		},
		{
			Provider:   models.IntegrationSalesforce,
			Name:       "Salesforce",
			Tagline:    "Sync leads, contacts, and email activity.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: "oauth",
			DocsURL:    "https://developer.salesforce.com/docs",
		},
		{
			Provider:   models.IntegrationPipedrive,
			Name:       "Pipedrive",
			Tagline:    "Persons, deals, and activity timeline.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: "oauth",
			DocsURL:    "https://developers.pipedrive.com",
		},
		{
			Provider:   models.IntegrationClose,
			Name:       "Close",
			Tagline:    "Leads, contacts, and inbox activity.",
			Category:   models.IntegrationCategoryCRM,
			AuthMethod: "api_key",
			DocsURL:    "https://developer.close.com",
		},

		// Automation
		{
			Provider:   models.IntegrationZapier,
			Name:       "Zapier",
			Tagline:    "Triggers and actions across 8,000+ apps.",
			Category:   models.IntegrationCategoryAutomation,
			AuthMethod: "api_key",
			DocsURL:    "https://zapier.com/apps",
		},
		{
			Provider:   models.IntegrationMake,
			Name:       "Make",
			Tagline:    "Visual automation scenarios.",
			Category:   models.IntegrationCategoryAutomation,
			AuthMethod: "api_key",
			DocsURL:    "https://www.make.com/en/integrations",
		},
		{
			Provider:   models.IntegrationN8N,
			Name:       "n8n",
			Tagline:    "Self-hosted automation workflows.",
			Category:   models.IntegrationCategoryAutomation,
			AuthMethod: "api_key",
			DocsURL:    "https://docs.n8n.io",
		},

		// Notifications
		{
			Provider:    models.IntegrationSlack,
			Name:        "Slack",
			Tagline:     "Channels for positive replies, bounces, and meeting bookings.",
			Category:    models.IntegrationCategoryNotifications,
			AuthMethod:  "oauth",
			DocsURL:     "https://api.slack.com",
			WebhookHint: "Incoming-webhook URL or OAuth app installation.",
		},
		{
			Provider:    models.IntegrationDiscord,
			Name:        "Discord",
			Tagline:     "Webhook-based notifications to a server channel.",
			Category:    models.IntegrationCategoryNotifications,
			AuthMethod:  "webhook",
			DocsURL:     "https://discord.com/developers/docs/resources/webhook",
			WebhookHint: "Paste a Discord channel webhook URL.",
		},

		// Meetings
		{
			Provider:    models.IntegrationCalendly,
			Name:        "Calendly",
			Tagline:     "Attribute booked meetings to the campaign that surfaced the lead.",
			Category:    models.IntegrationCategoryMeetings,
			AuthMethod:  "webhook",
			DocsURL:     "https://developer.calendly.com/api-docs/",
			WebhookHint: "Calendly POSTs invitee.created here.",
		},
		{
			Provider:    models.IntegrationCalCom,
			Name:        "Cal.com",
			Tagline:     "Same attribution path, open-source booking edition.",
			Category:    models.IntegrationCategoryMeetings,
			AuthMethod:  "webhook",
			DocsURL:     "https://cal.com/docs/core-features/webhooks",
			WebhookHint: "Cal.com POSTs BOOKING_CREATED events here.",
		},

		// Data
		{
			Provider:   models.IntegrationGoogleSheets,
			Name:       "Google Sheets",
			Tagline:    "Pull leads from a sheet, push reply / bounce / booked events back.",
			Category:   models.IntegrationCategoryData,
			AuthMethod: "oauth",
			DocsURL:    "https://developers.google.com/sheets/api",
			BetaFlag:   true,
		},
	}
}
