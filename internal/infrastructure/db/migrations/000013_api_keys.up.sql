-- API Keys table for programmatic access
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- Key identification
    name VARCHAR(255) NOT NULL,
    key_prefix VARCHAR(8) NOT NULL,      -- First 8 chars of key for display (e.g., "wmbly_ab")
    key_hash TEXT NOT NULL,               -- SHA-256 hash of full key

    -- Permissions (bitmask)
    permissions BIGINT NOT NULL DEFAULT 0,

    -- Scoping
    allowed_ips TEXT[],                   -- NULL means all IPs allowed
    allowed_email_accounts UUID[],        -- NULL means all accounts allowed

    -- Status and lifecycle
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- active, revoked, expired
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,               -- NULL means no expiration
    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for key lookup (by hash)
CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys(key_hash) WHERE status = 'active';

-- Index for user's keys
CREATE INDEX idx_api_keys_user ON api_keys(user_id, status);

-- Index for prefix lookup (for key display/identification)
CREATE INDEX idx_api_keys_prefix ON api_keys(user_id, key_prefix);

-- Index for organization's keys
CREATE INDEX idx_api_keys_org ON api_keys(organization_id, status);

-- API Key usage logs (for audit trail)
CREATE TABLE api_key_usage_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,

    endpoint VARCHAR(255) NOT NULL,
    method VARCHAR(10) NOT NULL,
    ip_address INET NOT NULL,
    user_agent TEXT,

    response_status INT,
    response_time_ms INT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for time-based cleanup
CREATE INDEX idx_api_key_usage_created ON api_key_usage_logs(created_at);

-- Index for key usage lookup
CREATE INDEX idx_api_key_usage_key ON api_key_usage_logs(api_key_id, created_at DESC);
