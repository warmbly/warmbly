-- Customer-defined routing rules for the premium warmup pool.
-- A rule says "when sender matches X, prefer recipients matching Y with
-- weight W". Used by selectWarmupPartner to bias selection toward
-- provider-consistent pairings (e.g. Gmail → Google Workspace senders)
-- without hard-excluding the rest of the pool.

CREATE TABLE warmup_routing_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    -- Lower priority value = evaluated first. The selector applies the
    -- highest-priority matching rule per (sender, recipient) candidate.
    priority INT NOT NULL DEFAULT 100,

    -- Match types: 'any', 'domain' (exact e.g. gmail.com), 'tld' (e.g. com),
    -- 'provider' (a logical bucket: google, microsoft, yahoo, apple, custom).
    sender_match_type TEXT NOT NULL CHECK (sender_match_type IN ('any', 'domain', 'tld', 'provider')),
    sender_match_value TEXT NOT NULL DEFAULT '',
    recipient_match_type TEXT NOT NULL CHECK (recipient_match_type IN ('any', 'domain', 'tld', 'provider')),
    recipient_match_value TEXT NOT NULL DEFAULT '',

    -- Multiplier on the selector's weighted score for matching pairs.
    -- > 1.0 = prefer; < 1.0 = discourage; 0 = exclude.
    weight REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0),

    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_warmup_routing_rules_org_priority
    ON warmup_routing_rules (organization_id, priority)
    WHERE enabled;
