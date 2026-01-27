-- Rate limit configurations per user (admin overridable)
CREATE TABLE user_rate_limits (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- Per-minute limits by endpoint category
    limit_read_pm INT NOT NULL DEFAULT 300,      -- GET requests per minute
    limit_write_pm INT NOT NULL DEFAULT 60,      -- POST/PATCH/DELETE per minute
    limit_bulk_pm INT NOT NULL DEFAULT 10,       -- Bulk operations per minute
    limit_unibox_pm INT NOT NULL DEFAULT 120,    -- Unibox API per minute
    limit_analytics_pm INT NOT NULL DEFAULT 60,  -- Analytics per minute

    -- Daily limits
    limit_api_calls_daily INT NOT NULL DEFAULT 50000,
    limit_bulk_ops_daily INT NOT NULL DEFAULT 100,

    -- Realtime/WebSocket limits
    limit_ws_message_pm INT NOT NULL DEFAULT 120,  -- Messages sent to client per minute
    limit_ws_join_pm INT NOT NULL DEFAULT 30,      -- Channel join attempts per minute
    limit_ws_event_pm INT NOT NULL DEFAULT 60,     -- Client-sent events per minute
    max_connections INT NOT NULL DEFAULT 10,       -- Max concurrent WebSocket connections

    -- Burst allowance (token bucket)
    burst_multiplier DECIMAL(3,2) NOT NULL DEFAULT 1.5,

    -- Admin notes
    notes TEXT,
    updated_by UUID REFERENCES users(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Plan-based default limits
CREATE TABLE plan_rate_limits (
    plan_id UUID PRIMARY KEY REFERENCES plans(id) ON DELETE CASCADE,

    -- Per-minute limits by endpoint category
    limit_read_pm INT NOT NULL DEFAULT 300,
    limit_write_pm INT NOT NULL DEFAULT 60,
    limit_bulk_pm INT NOT NULL DEFAULT 10,
    limit_unibox_pm INT NOT NULL DEFAULT 120,
    limit_analytics_pm INT NOT NULL DEFAULT 60,

    -- Daily limits
    limit_api_calls_daily INT NOT NULL DEFAULT 50000,
    limit_bulk_ops_daily INT NOT NULL DEFAULT 100,

    -- Realtime/WebSocket limits
    limit_ws_message_pm INT NOT NULL DEFAULT 120,
    limit_ws_join_pm INT NOT NULL DEFAULT 30,
    limit_ws_event_pm INT NOT NULL DEFAULT 60,
    max_connections INT NOT NULL DEFAULT 10,

    -- Burst allowance
    burst_multiplier DECIMAL(3,2) NOT NULL DEFAULT 1.5,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
