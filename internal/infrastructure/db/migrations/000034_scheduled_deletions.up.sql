-- Danger zone: track scheduled (soft) deletions with a grace window.
-- A resource gets marked here when a user requests deletion; the actual
-- hard delete happens after execute_after has passed, via a background job.

CREATE TABLE scheduled_deletions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- What is being deleted
    resource_type VARCHAR(32) NOT NULL,        -- 'organization' | 'user'
    resource_id   UUID        NOT NULL,

    -- Org context for filtering/notifications. NULL for user-scope deletions
    -- that the user wants to do across all their orgs.
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,

    -- Who initiated the deletion (for audit & cancellation auth)
    requested_by_user_id UUID NOT NULL REFERENCES users(id),

    -- Why (optional, for the team's records)
    reason TEXT,

    -- Lifecycle timestamps
    scheduled_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    execute_after  TIMESTAMPTZ NOT NULL,        -- earliest moment we may execute
    grace_days     INT         NOT NULL,        -- copy of grace window for display

    -- Status: pending | executing | completed | cancelled | failed
    status VARCHAR(16) NOT NULL DEFAULT 'pending',

    -- Cancellation
    cancelled_at         TIMESTAMPTZ,
    cancelled_by_user_id UUID REFERENCES users(id),
    cancelled_reason     TEXT,

    -- Execution
    executed_at      TIMESTAMPTZ,
    execution_error  TEXT,

    -- Reminders sent (bitmask: 1=initial, 2=7day, 4=24h, 8=completion)
    notifications_sent INT NOT NULL DEFAULT 0,
    last_reminder_at   TIMESTAMPTZ
);

-- Only one active pending deletion per resource at a time.
CREATE UNIQUE INDEX uq_scheduled_deletions_pending_resource
    ON scheduled_deletions (resource_type, resource_id)
    WHERE status = 'pending';

CREATE INDEX idx_scheduled_deletions_due
    ON scheduled_deletions (execute_after)
    WHERE status = 'pending';

CREATE INDEX idx_scheduled_deletions_org
    ON scheduled_deletions (organization_id);

CREATE INDEX idx_scheduled_deletions_user
    ON scheduled_deletions (requested_by_user_id);

-- Cached deletion timestamps on the parent rows so list endpoints and the
-- UI can show a banner without joining the deletions table.
ALTER TABLE organizations
    ADD COLUMN deletion_scheduled_at  TIMESTAMPTZ,
    ADD COLUMN deletion_scheduled_for TIMESTAMPTZ;

CREATE INDEX idx_organizations_pending_deletion
    ON organizations (deletion_scheduled_for)
    WHERE deletion_scheduled_for IS NOT NULL;

ALTER TABLE users
    ADD COLUMN deletion_scheduled_at  TIMESTAMPTZ,
    ADD COLUMN deletion_scheduled_for TIMESTAMPTZ;

CREATE INDEX idx_users_pending_deletion
    ON users (deletion_scheduled_for)
    WHERE deletion_scheduled_for IS NOT NULL;
