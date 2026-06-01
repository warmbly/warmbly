# Integrations — OAuth provider setup

Warmbly's integrations connect via **real OAuth 2.0** wherever the provider
supports it (HubSpot, Slack, Google Sheets, Pipedrive, Salesforce). The connect
flow, CSRF/PKCE handling, token exchange, encrypted-at-rest token storage, and
automatic refresh are all built in. The only thing the platform operator must
supply is **one developer app per provider**, registered in that provider's
console, plus its client ID/secret as environment variables.

Until a provider's credentials are present, it renders in the dashboard catalog
as **"Coming soon"** (the Connect button is disabled) — no code change is needed
to light it up; just add the env vars and restart the backend.

## How it works (no code changes required to enable a provider)

1. User clicks **Connect with X** in the dashboard.
2. SPA calls `POST /integrations/oauth/start` → backend mints a CSRF `state`
   (+ PKCE verifier where supported), stores it in `integration_oauth_states`,
   and returns the provider authorization URL.
3. SPA opens that URL in a popup. The user authorizes in the provider's UI.
4. The provider redirects to `GET /integrations/oauth/callback` (a public
   bouncer page) which `postMessage`s `{code, state}` back to the SPA opener.
5. SPA calls `POST /integrations/oauth/finish` → backend validates+consumes the
   `state`, exchanges the code for tokens, resolves the connected account, and
   stores the access/refresh tokens **sealed with the connecting user's
   envelope-encryption DEK** (KMS → per-user DEK → AES-GCM, the same path used
   for mailbox OAuth tokens). Plaintext tokens never touch a database column.

Access tokens are refreshed automatically (60s before expiry) using the stored
refresh token; if a refresh fails the connection flips to `reauth_required` and
the user is prompted to reconnect from the connection drawer.

## Required environment variables

Set these on the **backend** service. The redirect/callback URL must be allow-
listed in each provider's app config.

```
# Shared callback URL the providers redirect to. Defaults to
# $BACKEND_PUBLIC_URL/integrations/oauth/callback, else http://localhost:8080/...
INTEGRATIONS_OAUTH_REDIRECT_URL=https://api.yourdomain.com/integrations/oauth/callback

# HubSpot — https://developers.hubspot.com/  (create a public app)
HUBSPOT_OAUTH_CLIENT_ID=
HUBSPOT_OAUTH_CLIENT_SECRET=

# Slack — https://api.slack.com/apps  (OAuth & Permissions → bot scopes:
#   chat:write, channels:read, groups:read)
SLACK_OAUTH_CLIENT_ID=
SLACK_OAUTH_CLIENT_SECRET=

# Google Sheets — https://console.cloud.google.com/  (OAuth client, enable the
#   Google Sheets API; scopes: spreadsheets, userinfo.email)
GOOGLE_SHEETS_OAUTH_CLIENT_ID=
GOOGLE_SHEETS_OAUTH_CLIENT_SECRET=

# Pipedrive — https://developers.pipedrive.com/  (Marketplace Manager app;
#   scopes: contacts:full, deals:full)
PIPEDRIVE_OAUTH_CLIENT_ID=
PIPEDRIVE_OAUTH_CLIENT_SECRET=

# Salesforce — https://developer.salesforce.com/  (Connected App;
#   scopes: api, refresh_token)
SALESFORCE_OAUTH_CLIENT_ID=
SALESFORCE_OAUTH_CLIENT_SECRET=
```

The env var prefix is `<PROVIDER>_OAUTH_CLIENT_ID` / `_CLIENT_SECRET` — wiring a
new OAuth provider is just registering it in `internal/app/integration/oauth.go`
(`NewOAuthManager`) and adding the matching env vars.

## Redirect URL to register with each provider

```
<INTEGRATIONS_OAUTH_REDIRECT_URL>
# e.g. https://api.yourdomain.com/integrations/oauth/callback
```

For local development the default is `http://localhost:8080/integrations/oauth/callback`.

## Providers that do NOT use OAuth

- **Close** — no public OAuth app; the user pastes a Close API key.
- **Zapier / Make / n8n** — authenticate *into Warmbly* using a scoped Warmbly
  API key the user generates under Settings → API keys.
- **Discord** — the user pastes a channel webhook URL (outbound only).
- **Calendly / Cal.com** — inbound only; Warmbly mints a signed inbound URL the
  user pastes into the provider's webhook config.

All pasted secrets are sealed with the same envelope encryption as OAuth tokens
before they are stored — nothing sensitive is ever persisted in plaintext.

## How a user actually uses an integration

Connecting is step one; the value is the **automations** a user builds on a
connection. After connecting, the user opens the connection's management drawer
(click any connected card on the Integrations page) and adds rules:

> **When** a prospect replies — **only** positive replies, **≥60%** confidence —
> **notify** `#sales` with “🔥 {{contact_email}} is interested — {{subject}}”.

Each rule is fully customizable in the UI (no code, no API keys to paste):

- **Trigger** — which Warmbly event fires the rule (reply, bounce, unsubscribe,
  warmup-health, complaint, meeting booked).
- **Filters** (reply triggers) — restrict to specific reply intents
  (`positive`, `question`, `negative`, …) and a minimum classifier confidence.
- **Destination** — Slack channel, Google Sheet ID, or an outbound URL,
  depending on the provider.
- **Message template** — a custom string with `{{placeholder}}` substitution
  over the event payload (`{{contact_email}}`, `{{subject}}`, `{{intent}}`,
  `{{campaign_id}}`, `{{reason}}`, …).

Rules are stored in `integration_event_subscriptions.config` (JSONB) and applied
at dispatch time by `internal/app/integration/dispatch.go`
(`subscriptionMatchesFilter`, `renderTemplate`).

## Event-driven actions — the wiring

Examples of trigger → action pairs:

- `campaign.reply_received` → `slack.notify` (ping #sales)
- `campaign.reply_received` → `hubspot.upsert_contact` (create/update + log note)
- `campaign.email_bounced` → `discord.notify`
- meeting booked (Calendly/Cal.com) → Slack / CRM

Events reach integration actions through the webhook dispatch sink
(`webhook.Service.WireDispatchSink`), so any event already delivered to customer
webhooks also drives integration actions. The sink is wired in **both** binaries:

- **`cmd/backend`** — deliverability ingest (bounce/complaint/unsubscribe),
  meeting-booked, email-account lifecycle, warmup health.
- **`cmd/consumer`** — inbound campaign replies. The consumer is where replies
  are detected (`advanced.ProcessIncomingReply`), which now emits
  `campaign.reply_received` with `intent`/`confidence` in the payload. This is
  what makes "notify me when a prospect replies → Slack/CRM" fire for real.

`advanced.Service` exposes `WireDispatcher(EventDispatcher)` (see
`internal/app/advanced/events.go`); `ProcessIncomingReply` and
`IngestDeliverabilityEvent` call `emit(...)` to fan their events out.

Each executed action is recorded as an `integration_sync_runs` row for
observability and surfaced in the connection drawer's "Recent activity".

## Access / gating

Integrations are a **paid-plan** feature (enforced in the integration handlers
via `FeatureGateService.IsPaidOrganization`). Browsing the catalog is open so
non-paid orgs see what's available; connecting requires an active paid plan.
