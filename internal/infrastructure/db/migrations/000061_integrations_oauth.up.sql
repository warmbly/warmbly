-- Enterprise integrations: real OAuth connect flows, encrypted-at-rest
-- provider secrets, a richer connection lifecycle + health model, event-driven
-- actions (e.g. positive reply -> Slack ping / HubSpot contact), CRM field
-- mappings, and sync-run history. Builds on 000053 (integration_connections,
-- meeting_bookings).
--
-- Secret handling: access/refresh tokens are sealed with the connecting user's
-- envelope-encryption DEK (KMS -> per-user DEK -> AES-GCM, same path the email
-- mailbox OAuth tokens use) and stored as base64 ciphertext. Plaintext secrets
-- never land in any column here. The legacy config_encrypted blob is likewise
-- now sealed in app code rather than stored as plaintext JSON.

-- ---------------------------------------------------------------------------
-- 1. Extend integration_connections with OAuth + lifecycle + health columns.
-- ---------------------------------------------------------------------------
ALTER TABLE integration_connections
    ADD COLUMN IF NOT EXISTS connected_by_user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS auth_method             TEXT NOT NULL DEFAULT 'api_key',
    ADD COLUMN IF NOT EXISTS access_token_encrypted  TEXT,
    ADD COLUMN IF NOT EXISTS refresh_token_encrypted TEXT,
    ADD COLUMN IF NOT EXISTS token_expires_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS granted_scopes          TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS external_account_id     TEXT,
    ADD COLUMN IF NOT EXISTS external_account_name   TEXT,
    ADD COLUMN IF NOT EXISTS health                  TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS health_detail           TEXT,
    ADD COLUMN IF NOT EXISTS health_checked_at       TIMESTAMPTZ;

-- Widen the lifecycle: 'authorizing' (OAuth handshake mid-flight) and
-- 'reauth_required' (token revoked/expired; the user must reconnect).
ALTER TABLE integration_connections DROP CONSTRAINT IF EXISTS integration_connections_status_check;
ALTER TABLE integration_connections
    ADD CONSTRAINT integration_connections_status_check
    CHECK (status IN ('pending', 'authorizing', 'connected', 'degraded', 'reauth_required', 'disconnected'));

-- ---------------------------------------------------------------------------
-- 2. OAuth handshake state (CSRF protection + PKCE). One short-lived row per
--    connect attempt; consumed on callback.
-- ---------------------------------------------------------------------------
CREATE TABLE integration_oauth_states (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,

    -- Opaque random nonce echoed back by the provider; the join key on callback.
    state TEXT NOT NULL UNIQUE,

    -- PKCE verifier (RFC 7636). Empty for providers that don't support PKCE.
    code_verifier TEXT NOT NULL DEFAULT '',

    -- User-chosen connection label, carried through the round trip.
    label TEXT NOT NULL DEFAULT '',
    requested_scopes TEXT[] NOT NULL DEFAULT '{}',

    used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_integration_oauth_states_expires ON integration_oauth_states (expires_at);

-- ---------------------------------------------------------------------------
-- 3. Event-driven actions. Maps a Warmbly platform event to a provider action
--    on a connection, e.g. campaign.reply_received -> slack.notify (channel)
--    or hubspot.upsert_contact. Non-secret action params live in config.
-- ---------------------------------------------------------------------------
CREATE TABLE integration_event_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    event_type TEXT NOT NULL,   -- a models.WebhookEventType value (shared event vocabulary)
    action TEXT NOT NULL,       -- provider action key, e.g. 'slack.notify', 'hubspot.upsert_contact'
    config JSONB NOT NULL DEFAULT '{}'::jsonb,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (connection_id, event_type, action)
);

CREATE INDEX idx_integration_event_subs_dispatch
    ON integration_event_subscriptions (organization_id, event_type)
    WHERE enabled;

-- ---------------------------------------------------------------------------
-- 4. CRM field mappings (Warmbly contact field <-> external object field).
-- ---------------------------------------------------------------------------
CREATE TABLE integration_field_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    direction TEXT NOT NULL DEFAULT 'push' CHECK (direction IN ('push', 'pull', 'both')),
    warmbly_field TEXT NOT NULL,
    external_field TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (connection_id, direction, warmbly_field, external_field)
);

-- ---------------------------------------------------------------------------
-- 5. Sync-run history for observability (connect, token refresh, dispatch).
-- ---------------------------------------------------------------------------
CREATE TABLE integration_sync_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    kind TEXT NOT NULL,         -- 'oauth_connect','token_refresh','event_dispatch','manual_sync'
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'success', 'error')),
    detail TEXT NOT NULL DEFAULT '',
    records_processed INT NOT NULL DEFAULT 0,

    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX idx_integration_sync_runs_conn ON integration_sync_runs (connection_id, started_at DESC);
