ALTER TABLE org_ai_settings
    DROP COLUMN IF EXISTS ai_overage_monthly_limit_micro,
    DROP COLUMN IF EXISTS ai_overage_enabled;
