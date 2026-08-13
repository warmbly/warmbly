-- Postgres cannot drop a single enum value, so 'paused_guardrail' stays on
-- campaign_status. Any campaign still parked in it is returned to a plain
-- pause first, so nothing is left in a status the application no longer knows.
UPDATE campaigns SET status = 'paused' WHERE status = 'paused_guardrail';

DROP INDEX IF EXISTS idx_campaigns_guardrail_active;

ALTER TABLE campaigns DROP CONSTRAINT IF EXISTS campaigns_guardrail_bounds;

ALTER TABLE campaigns
    DROP COLUMN IF EXISTS guardrail_enabled,
    DROP COLUMN IF EXISTS guardrail_bounce_rate_max,
    DROP COLUMN IF EXISTS guardrail_complaint_rate_max,
    DROP COLUMN IF EXISTS guardrail_reply_rate_min,
    DROP COLUMN IF EXISTS guardrail_min_sample,
    DROP COLUMN IF EXISTS guardrail_window_days,
    DROP COLUMN IF EXISTS guardrail_tripped_at,
    DROP COLUMN IF EXISTS guardrail_reason;

DROP INDEX IF EXISTS idx_email_account_daily_plan_date;
DROP TABLE IF EXISTS email_account_daily_plan;
DROP TABLE IF EXISTS email_account_behavior;
