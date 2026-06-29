# Warmbly for Zapier

The Warmbly Zapier integration, built with the [Zapier Platform CLI](https://platform.zapier.com/) (TypeScript). It exposes Warmbly triggers, actions, and searches over the public API at `https://api.warmbly.com/v1`, authenticated with OAuth2.

The customer-facing guide lives at [docs.warmbly.com/guides/zapier](https://docs.warmbly.com/guides/zapier/).

## What's inside

- **Auth**: OAuth2 authorization-code with rotating refresh tokens. The connection test and label come from `GET /v1/me`.
- **Triggers** (polling): New Contact, New or Updated Contact, New Email Received (Unibox), New Meeting Booked, New Deal, Deal Won, New CRM Task, New Campaign, Campaign Completed, New Mailbox Connected. Plus hidden list triggers that power campaign / mailbox / pipeline / stage / template dropdowns.
- **Creates**: Create or Update Contact, Update Contact, Add Contact to Campaign, Create Contact Note, Send Email, Reply in Inbox, Create Deal, Update Deal, Create CRM Task, Create Campaign, Start Campaign, Stop Campaign, Create Reply Template, Verify Email Address.
- **Searches**: Find Contact, Find Campaign, Find Mailbox, Find Reply Template.

### Why triggers poll instead of using webhooks

Warmbly's webhook subscriptions created by an OAuth app must echo a verification challenge back on a server-to-server `webhook.test` POST (see `internal/app/webhook/service.go`). Zapier's catch-hook URL returns `200` but cannot echo an arbitrary challenge token, so an OAuth-created REST Hook would never verify and never receive events. Polling is therefore the reliable mechanism: each trigger reads a list endpoint on Zapier's schedule and Zapier dedupes by `id`.

Instant (webhook) triggers are possible with a small additive backend change: an authenticated `POST /v1/webhooks/:id/confirm` that lets the credential owner verify the endpoint without the async echo. See the integration design notes / PR description.

### Polling caveats

Each trigger reads the first ~100 rows of a list endpoint. Triggers whose list is ordered by the same field as the event are fully reliable at any volume:

- newest-first by `created_at`: New Contact, New Deal, New CRM Task, New Campaign, New Mailbox, New Email Received (`internal_date`). New or Updated Contact sorts by `updated_at` so edits re-surface.

Triggers where the event time differs from the list order are best-effort and can miss the tail only in high-volume workspaces:

- **New Meeting Booked** lists by `scheduled_for DESC`, not booking time, so a new booking for a near date could fall past the first 100 if more than 100 future-dated meetings exist.
- **Campaign Completed** and **Deal Won** poll creation-ordered lists and filter by status, so a status change on a much older record can fall past the first 100.

The robust fix for all three is instant webhook triggers (see above). For typical workspaces (well under 100 future meetings / open deals) polling catches everything.

## Prerequisites

1. Register an OAuth application in the Warmbly dashboard (`POST /v1/oauth/applications`, or Settings → Developer → OAuth apps). Set the redirect URI to Zapier's callback:
   ```
   https://zapier.com/dashboard/auth/oauth/return/App<APP_ID>CLIAPI/
   ```
   (Zapier shows the exact value under your integration's Authentication settings.)
2. Note the `client_id` (`wmcid_…`) and `client_secret` (`wmcs_…`, shown once).
3. Request these scopes on the app: `read_emails send_campaigns read_campaigns write_campaigns read_contacts write_contacts read_unibox write_unibox read_crm write_crm read_templates write_templates`.

## Environment

Set these on the Zapier app (`zapier env:set <version> KEY=value`) or locally via a `.env` file:

| Variable | Purpose | Default |
|----------|---------|---------|
| `CLIENT_ID` | OAuth client id | — |
| `CLIENT_SECRET` | OAuth client secret | — |
| `WARMBLY_API_BASE` | API base URL | `https://api.warmbly.com/v1` |
| `WARMBLY_APP_BASE` | Dashboard base URL (consent screen) | `https://app.warmbly.com` |

## Develop

```bash
npm install
npm run typecheck     # tsc --noEmit
npm run lint          # eslint
npm test              # jest (includes a full Zapier schema validation)
npm run build         # compile src -> dist
npm run validate      # build + zapier validate (requires the zapier CLI + login)
npm run push          # build + zapier push (deploy a new version)
```

`npm test` validates the entire app definition against `zapier-platform-schema`, so a structural mistake fails locally before any push.

## Layout

```
src/
  index.ts            App definition (wires triggers/creates/searches)
  authentication.ts   OAuth2 config + GET /v1/me test and connection label
  lib/
    client.ts         base URLs, scopes, auth + error middleware, helpers
    poll.ts           polling-trigger factory for list endpoints
  triggers/           polling triggers + hidden dropdown list triggers
  creates/            actions
  searches/           lookups
test/                 jest tests
```
