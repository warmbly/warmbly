ALTER TABLE org_ai_settings
    DROP COLUMN IF EXISTS member_limit_daily,
    DROP COLUMN IF EXISTS member_limit_weekly;
ALTER TABLE organizations DROP COLUMN IF EXISTS assistant_shared_history;
DROP INDEX IF EXISTS idx_agent_sessions_org;
