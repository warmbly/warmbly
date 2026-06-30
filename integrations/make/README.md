# Warmbly for Make

The Warmbly [Make.com](https://www.make.com/) Custom App, built in the Make Apps SDK format (IML / `.iml.json`). It exposes Warmbly triggers, actions, and searches over the public API at `https://api.warmbly.com/v1`, authenticated with OAuth2.

The customer-facing guide lives at [docs.warmbly.com/guides/make](https://docs.warmbly.com/guides/make/).

## What's inside

This is the [Make Apps local development](https://developers.make.com/custom-apps-documentation/make-apps-editor/apps-sdk/local-development-for-apps) layout: a single `makecomapp.json` manifest that references every component's code files. Each component lives in its own directory.

- **Connection** (`connections/warmbly/`): OAuth2 authorization-code with rotating refresh tokens. The connection test and label come from `GET /v1/me` (`{{organization_name}} ({{email}})`). Scopes are least-privilege: contacts, campaigns, mailboxes, the inbox, CRM, templates, and read-only analytics.
- **Base** (`general/base.iml.json`): sets the API base URL, the bearer `Authorization` header on every request, and the shared error handler (surfaces the API `code` and `request_id`). Module `api.iml.json` files use relative paths.
- **Modules** (`modules/`, 57 total):
  - **Triggers** (10, polling): New Contact, New or Updated Contact, New Email Received (Unibox), New Meeting Booked, New Deal, Deal Won, New CRM Task, New Campaign, Campaign Completed, New Mailbox Connected.
  - **Actions** (38): contacts (create-or-update, update, delete, add/remove campaign, note create/update/delete), email and inbox (send, reply, verify, mark seen), CRM (deal create/update/delete, task create/update/delete), campaigns (create, update, delete, start, stop), mailboxes (update, delete, warmup start/pause/resume/stop), templates (create, update, delete, render), meetings (log, delete), groups (create category/tag/folder).
  - **Searches** (9): Find Contact, Find Deal, Find CRM Task, Find Campaign, Find Mailbox, Find Meeting, Find Reply Template, Get Campaign Analytics, Get Dashboard Analytics.
- **RPCs** (`rpcs/`, 6): remote procedure calls that power the dynamic dropdowns referenced from inputs as `rpc://<name>`: `campaignList`, `mailboxList`, `pipelineList`, `stageList` (depends on the selected pipeline), `templateList`, `contactList`.
- **Functions** (`functions/`, 1): `filterByStatus`, used by Campaign Completed to filter the campaign list client-side (the `/campaigns` endpoint has no status query param).

### How triggers work

Triggers poll: each one reads a list endpoint on the scenario's schedule and Make dedupes by the `trigger.id`, so a scenario runs once per new record. Each trigger surfaces the most recent rows (limit 100), newest-first.

Triggers ordered by the same field as the event are reliable at any volume:

- newest-first by `created_at`: New Contact, New Deal, New CRM Task, New Campaign, New Mailbox, New Email Received (`internal_date`). New or Updated Contact sorts by `updated_at` so edits re-surface (its `trigger.id` combines the contact id with the update time).

Triggers where the event time differs from the list order are best-effort and can miss the tail only in very high-volume workspaces:

- **New Meeting Booked** lists by `scheduled_for DESC`, not booking time.
- **Campaign Completed** and **Deal Won** poll creation-ordered lists and filter by status (`filterByStatus` and the `status=won` query, respectively), so a status change on a much older record can fall past the first 100.

For typical workspaces, polling catches everything.

## Prerequisites

1. Register an OAuth application in the Warmbly dashboard (`POST /v1/oauth/applications`, or Settings -> Developer -> OAuth apps). Set the redirect URI to Make's callback:
   ```
   https://www.make.com/oauth/cb/app
   ```
   (Make shows the exact value under your app's connection settings; self-hosted/white-label Make instances differ.)
2. Note the `client_id` (`wmcid_...`) and `client_secret` (`wmcs_...`, shown once).
3. Request these scopes on the app: `read_emails write_emails send_campaigns read_campaigns write_campaigns read_contacts write_contacts bulk_contacts read_unibox write_unibox read_crm write_crm read_templates write_templates read_analytics`.

## Credentials (where the client secret goes)

This is a public repository, so the OAuth client id and secret are **never committed**. The `common` code files (`general/common.json` and `connections/warmbly/common.json`) ship with empty placeholders and must stay empty; the IML reads them as `{{common.clientId}}` / `{{common.clientSecret}}`.

The real secret lives only in the Make app's **Common Data**, stored server-side by Make and never returned in plaintext (a clone pulls it back masked, which is why the committed files are empty). Set it once:

- **Recommended:** in the Make UI, open the app and enter `clientId` / `clientSecret` under **Common Data**. Nothing touches the repo.
- If you `Deploy to Make` from a local clone instead, fill the values in your local `common.json` files and run `git update-index --skip-worktree general/common.json connections/warmbly/common.json` first, so git ignores your local edits. Never `git add` a filled `common.json`.

`.gitignore` also excludes `*.local.json` for any local secret copies.

## Layout

```
makecomapp.json          App manifest (lists every component + its code files)
general/
  base.iml.json          base URL, auth header, shared error handler
  common.json            clientId / clientSecret (empty placeholders)
  groups.json            module grouping for the UI
connections/warmbly/
  api.iml.json           OAuth authorize / token / refresh / info (GET /v1/me)
  scope.iml.json         default requested scopes
  scopes.iml.json        scope catalog (name -> description)
  common.json            clientId / clientSecret (empty placeholders)
  parameters.iml.json    connection parameters (none for the managed app)
modules/<moduleKey>/
  api.iml.json           communication (request + response)
  parameters.iml.json    mappable input fields
  interface.iml.json     output fields
  samples.iml.json       sample output
rpcs/<rpcName>/
  api.iml.json           dropdown source (label/value)
  parameters.iml.json    rpc parameters
functions/filterByStatus/
  code.js  test.js       IML helper used by Campaign Completed
scripts/
  validate.mjs           structural validator (runs in CI)
```

## Develop and deploy

Use the [Make Apps Editor](https://marketplace.visualstudio.com/items?itemName=Integromat.apps-sdk) VS Code extension: open this folder, it reads `makecomapp.json`, and you can edit components locally and deploy to your Make organization. You can also build the app in the online Apps editor and paste each component's IML.

There is no compile step: IML files are interpreted by Make. The structural check is `validate.mjs`, which CI runs on every change to this folder:

```bash
node scripts/validate.mjs   # or: npm run validate
```

It verifies that every `.iml.json` / `.json` parses, the function JS parses, every `codeFiles` path in the manifest exists (and uses a valid key), there are no orphaned component files, each module declares a valid `moduleType` / `actionCrud`, and the connection a valid `connectionType`.
