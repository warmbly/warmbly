-- Per-member AI limits for all three windows (daily/weekly join the monthly
-- ceiling from 000073), and the org-wide shared-assistant-history switch: when
-- on, every member with the use-AI permission sees and can continue every
-- conversation in the workspace instead of only their own.
ALTER TABLE org_ai_settings
    ADD COLUMN IF NOT EXISTS member_limit_daily INTEGER CHECK (member_limit_daily IS NULL OR member_limit_daily > 0),
    ADD COLUMN IF NOT EXISTS member_limit_weekly INTEGER CHECK (member_limit_weekly IS NULL OR member_limit_weekly > 0);

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS assistant_shared_history BOOLEAN NOT NULL DEFAULT FALSE;

-- Shared mode lists sessions org-wide.
CREATE INDEX IF NOT EXISTS idx_agent_sessions_org
    ON agent_sessions (org_id, created_at DESC);
