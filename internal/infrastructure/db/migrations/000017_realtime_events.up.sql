-- Realtime events table for critical event persistence and catch-up delivery
CREATE TABLE IF NOT EXISTS realtime_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    priority VARCHAR(10) NOT NULL DEFAULT 'normal',
    payload JSONB NOT NULL,
    delivered BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours'
);

-- Index for fetching undelivered events for a user
CREATE INDEX idx_realtime_events_user_pending ON realtime_events(user_id, created_at)
    WHERE delivered = FALSE;

-- Index for fetching undelivered events for an organization
CREATE INDEX idx_realtime_events_org ON realtime_events(org_id, created_at)
    WHERE delivered = FALSE;

-- Index for cleanup job (expired events)
CREATE INDEX idx_realtime_events_expires ON realtime_events(expires_at)
    WHERE delivered = FALSE;

-- Index for event type filtering
CREATE INDEX idx_realtime_events_type ON realtime_events(event_type, created_at DESC);

COMMENT ON TABLE realtime_events IS 'Stores critical realtime events for catch-up delivery when clients reconnect';
COMMENT ON COLUMN realtime_events.priority IS 'Event priority: critical (persisted) or normal (ephemeral)';
COMMENT ON COLUMN realtime_events.delivered IS 'Whether the event has been delivered to all active connections';
COMMENT ON COLUMN realtime_events.expires_at IS 'Events older than this are cleaned up automatically';
