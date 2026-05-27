-- Customer-facing webhooks: each organization can subscribe to one or more
-- endpoint URLs, filter by event type, and receive HMAC-signed POSTs when
-- platform events fire (campaign sends, replies, warmup state changes,
-- deliverability signals, etc).

CREATE TABLE webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',

    -- Shared secret used to sign delivery payloads with HMAC-SHA256.
    -- Stored opaquely; rotated by clients via the API. Never returned in
    -- list responses except once at creation time.
    secret TEXT NOT NULL,

    -- Subscribed event types. Empty array = subscribe to all events.
    -- Examples: 'campaign.reply_received', 'warmup.health_changed'.
    event_types TEXT[] NOT NULL DEFAULT '{}',

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    -- Last delivery health snapshot (updated by the dispatcher).
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    last_failure_reason TEXT,
    consecutive_failures INT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhook_endpoints_org ON webhook_endpoints (organization_id)
    WHERE enabled;
CREATE INDEX idx_webhook_endpoints_event_types ON webhook_endpoints USING GIN (event_types)
    WHERE enabled;

-- Per-delivery state machine. Each (event, endpoint) pair gets a row; the
-- dispatcher picks rows in 'pending' or 'retry' state, POSTs the payload,
-- and updates status. Failed deliveries are retried with exponential
-- backoff up to max_attempts.
CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    event_type TEXT NOT NULL,
    event_id UUID NOT NULL,
    payload JSONB NOT NULL,

    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'in_flight', 'delivered', 'failed', 'abandoned')),

    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 8,

    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_attempt_at TIMESTAMPTZ,

    response_status INT,
    response_body_excerpt TEXT,
    error_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhook_deliveries_due
    ON webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'retry');

CREATE INDEX idx_webhook_deliveries_endpoint
    ON webhook_deliveries (endpoint_id, created_at DESC);
