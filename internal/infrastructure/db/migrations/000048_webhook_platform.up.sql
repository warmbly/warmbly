-- Full webhook platform: per-OAuth-app webhook-domain allowlist, app-scoped
-- endpoints, endpoint ownership verification, and auto-disable bookkeeping.

-- An OAuth app may only register webhook endpoints on domains it declares here.
-- Subdomain-inclusive matching is applied in app code (a leading-dot entry
-- ".acme.com" matches any subdomain; a bare "acme.com" matches that host only).
-- Empty list = the app cannot register webhooks.
ALTER TABLE oauth_applications
    ADD COLUMN allowed_webhook_domains text[] NOT NULL DEFAULT '{}';

-- Endpoint provenance + ownership-verification + auto-disable state.
ALTER TABLE webhook_endpoints
    -- App-scoped endpoint: when set, the endpoint is owned by an OAuth app and
    -- its URL host is re-checked against the app's allowed_webhook_domains on
    -- every create/update/delivery. NULL = a normal org-level endpoint.
    ADD COLUMN oauth_application_id uuid REFERENCES oauth_applications (id) ON DELETE CASCADE,
    -- The member who created the endpoint (UX/audit; NULL for app-created).
    ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    -- Ownership verification: an endpoint only receives the real event stream
    -- once verified. Until then it gets a single signed challenge request, so a
    -- webhook pointed at a non-consenting third party can never be flooded.
    ADD COLUMN verified_at timestamptz,
    -- True once the receiver echoed the challenge back (proves URL control, not
    -- just reachability). Reachability (a 2xx to the challenge) sets verified_at;
    -- echo additionally sets this. App-scoped endpoints REQUIRE echo to verify.
    ADD COLUMN ownership_confirmed boolean NOT NULL DEFAULT false,
    -- Current challenge value the receiver must echo (rotated on each (re)verify).
    ADD COLUMN verification_token text NOT NULL DEFAULT '',
    -- When the current continuous-failure streak began (NULL when healthy). Used
    -- for hysteretic auto-disable: we only disable after sustained failure.
    ADD COLUMN first_failure_at timestamptz,
    -- Set when the platform auto-disabled the endpoint after sustained failure.
    ADD COLUMN auto_disabled_at timestamptz,
    -- Human-readable reason shown in the dashboard when auto-disabled.
    ADD COLUMN disabled_reason text;

-- Grandfather every pre-existing endpoint as verified: they were already
-- receiving the event stream before ownership verification existed, so do not
-- silently cut them off. New endpoints created after this migration go through
-- the challenge flow (verified_at stays NULL until they pass).
UPDATE webhook_endpoints
SET verified_at = created_at, ownership_confirmed = true
WHERE verified_at IS NULL;

CREATE INDEX idx_webhook_endpoints_oauth_app ON webhook_endpoints (oauth_application_id)
    WHERE oauth_application_id IS NOT NULL;

-- Reaper support: claim stuck in_flight rows by age.
CREATE INDEX idx_webhook_deliveries_inflight ON webhook_deliveries (updated_at)
    WHERE status = 'in_flight';

-- Visible throttle drops: when the per-(org,event_type) dispatch throttle trips,
-- we record a flood-safe daily rollup (one upsert per minute-window-trip) so the
-- dashboard can show "events were rate-limited" instead of dropping silently.
CREATE TABLE webhook_event_drops (
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    event_type text NOT NULL,
    day date NOT NULL,
    dropped_windows integer NOT NULL DEFAULT 0,
    last_dropped_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, event_type, day)
);
