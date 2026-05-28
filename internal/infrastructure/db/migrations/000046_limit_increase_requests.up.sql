-- Limit-increase request workflow. Sits on top of the override system
-- added in 000044: a user submits a request from the dashboard, an
-- admin reviews it, and on approval the corresponding column on
-- organization_limit_overrides is bumped to the requested value via
-- the same SetLimitOverrides write path.
--
-- A unique partial index on (organization_id, field) WHERE status =
-- 'pending' prevents one org from spamming the queue with duplicate
-- requests for the same resource. Cancelled / rejected / approved
-- rows are kept indefinitely as the historical record.
--
-- requested MUST be greater than current_effective so the queue never
-- carries no-op or downgrade requests — those should not consume
-- review attention.

CREATE TYPE limit_request_status AS ENUM (
    'pending',
    'approved',
    'rejected',
    'cancelled'
);

CREATE TABLE limit_increase_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    field VARCHAR(64) NOT NULL,
    current_effective INT NOT NULL,
    requested INT NOT NULL,
    reason TEXT NOT NULL,
    status limit_request_status NOT NULL DEFAULT 'pending',
    submitted_by UUID NOT NULL REFERENCES users(id),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    review_notes TEXT NOT NULL DEFAULT '',

    CONSTRAINT requested_positive CHECK (requested > 0),
    CONSTRAINT requested_larger_than_current CHECK (requested > current_effective)
);

-- One open request per (org, field).
CREATE UNIQUE INDEX uq_limit_requests_one_pending_per_field
    ON limit_increase_requests(organization_id, field)
    WHERE status = 'pending';

CREATE INDEX idx_limit_requests_status ON limit_increase_requests(status, submitted_at DESC);
CREATE INDEX idx_limit_requests_org ON limit_increase_requests(organization_id, submitted_at DESC);
