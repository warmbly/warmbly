-- Per-organization limit overrides. A row exists only for orgs that an
-- admin has explicitly touched. Each numeric column uses 0 as the "no
-- override, inherit from plan" sentinel so the effective limit query
-- stays branch-free:
--
--     COALESCE(NULLIF(override, 0), plan_limit)
--
-- Reverting an override is a write of 0 rather than a DELETE, which
-- keeps the granted_by / granted_at audit trail intact across revisions.

CREATE TABLE organization_limit_overrides (
    organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,

    max_campaigns        INT NOT NULL DEFAULT 0,
    max_active_campaigns INT NOT NULL DEFAULT 0,
    max_team_members     INT NOT NULL DEFAULT 0,
    max_email_accounts   INT NOT NULL DEFAULT 0,
    max_contacts         INT NOT NULL DEFAULT 0,
    daily_campaign_limit INT NOT NULL DEFAULT 0,

    granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes      TEXT NOT NULL DEFAULT '',

    CONSTRAINT non_negative_overrides CHECK (
        max_campaigns        >= 0 AND
        max_active_campaigns >= 0 AND
        max_team_members     >= 0 AND
        max_email_accounts   >= 0 AND
        max_contacts         >= 0 AND
        daily_campaign_limit >= 0
    )
);

CREATE INDEX idx_org_limit_overrides_granted_by ON organization_limit_overrides(granted_by);
