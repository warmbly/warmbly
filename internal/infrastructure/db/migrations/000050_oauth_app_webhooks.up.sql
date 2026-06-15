-- App-level webhook subscriptions (the GitHub/Slack-app model): an OAuth app
-- declares a webhook URL + the events it wants + one signing secret. When an org
-- authorizes the app, Warmbly materializes a managed webhook_endpoints row for
-- that org (scoped to the permissions the org granted), so every delivery flows
-- through the existing delivery pipeline (retries, signing, delivery log,
-- redelivery) and is fully inspectable per app. This lets apps receive events
-- over webhooks instead of holding a websocket connection per install.
ALTER TABLE oauth_applications
    -- Default webhook URL for app-materialized endpoints. Must fall inside the
    -- app's allowed_webhook_domains. Empty = the app does not use webhooks.
    ADD COLUMN webhook_url text NOT NULL DEFAULT '',
    -- Event types the app subscribes to. Empty = all non-firehose events the
    -- granting org's scopes allow.
    ADD COLUMN webhook_events text[] NOT NULL DEFAULT '{}',
    -- One signing secret across all of the app's materialized endpoints, so the
    -- app verifies every delivery (from any org) with a single key.
    ADD COLUMN webhook_secret text NOT NULL DEFAULT '';

-- At most one materialized endpoint per (app, org), so reconciliation can upsert.
CREATE UNIQUE INDEX idx_webhook_endpoints_app_org_unique
    ON webhook_endpoints (oauth_application_id, organization_id)
    WHERE oauth_application_id IS NOT NULL;
