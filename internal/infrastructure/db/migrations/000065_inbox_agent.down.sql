DROP TABLE IF EXISTS ai_thread_drafts;

ALTER TABLE organizations
    DROP COLUMN IF EXISTS inbox_agent_enabled;
