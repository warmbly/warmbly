-- Third-party integration connection state. Each row is one org's link to
-- one provider (HubSpot, Salesforce, Pipedrive, Close, Zapier, Make, n8n,
-- Slack, Discord, Calendly, Cal.com, Google Sheets). Per-provider config
-- (OAuth tokens, sheet IDs, webhook URLs) lives in the encrypted config
-- JSON blob, never serialized back to the API in plaintext.

CREATE TABLE integration_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- Provider key (e.g. 'hubspot', 'salesforce', 'calendly', 'slack').
    -- Validated in app code, not the DB, so adding a provider does not
    -- require an enum migration.
    provider TEXT NOT NULL,

    -- Human-readable label set by the user (e.g. "Main HubSpot workspace").
    -- Defaults to the provider key if not set.
    label TEXT NOT NULL DEFAULT '',

    -- Operational state, drives the badge on the dashboard.
    --   pending     : record exists but connection not finished (OAuth mid-flight)
    --   connected   : healthy, last interaction succeeded
    --   degraded    : connected but last call errored
    --   disconnected: token revoked or auth failure
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'connected', 'degraded', 'disconnected')),

    -- Inbound webhook secret for providers that POST to us (Calendly,
    -- Cal.com). Per-org-per-provider so a leaked secret only affects one
    -- customer.
    inbound_secret TEXT,

    -- Encrypted provider-specific config. Shape is per-provider.
    -- Plaintext is never returned to the API consumer.
    config_encrypted BYTEA,

    -- Public display fields, what the UI shows next to "connected" state.
    -- Never includes secrets. Examples: connected account email, sheet
    -- title, workspace name.
    display_fields JSONB NOT NULL DEFAULT '{}'::jsonb,

    last_synced_at TIMESTAMPTZ,
    last_error TEXT,
    last_error_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (organization_id, provider, label)
);

CREATE INDEX idx_integration_connections_org
    ON integration_connections (organization_id, provider);

-- Calendly + Cal.com bookings. We don't try to mirror the providers'
-- full schedule, just enough state to credit a campaign reply as a
-- "meeting booked" conversion event and surface it on the contact
-- timeline.
CREATE TABLE meeting_bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    source TEXT NOT NULL,  -- 'calendly' | 'cal_com'

    -- Provider's event identifier, used to dedupe replays of the
    -- "invitee.created" webhook.
    external_event_id TEXT NOT NULL,

    invitee_email TEXT NOT NULL,
    invitee_name TEXT NOT NULL DEFAULT '',
    event_name TEXT NOT NULL DEFAULT '',

    scheduled_for TIMESTAMPTZ,

    -- Joined to a Warmbly contact + campaign if we can match the email.
    contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,

    raw_payload JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, source, external_event_id)
);

CREATE INDEX idx_meeting_bookings_contact ON meeting_bookings (contact_id);
CREATE INDEX idx_meeting_bookings_recent ON meeting_bookings (organization_id, created_at DESC);
