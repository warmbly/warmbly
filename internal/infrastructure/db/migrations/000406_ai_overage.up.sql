-- Whether AI may spend money the workspace added once the plan's included
-- allowance is used up, and the most it may take from it per calendar month
-- (NULL: no cap). Existing workspaces keep today's behaviour (on, uncapped).
ALTER TABLE org_ai_settings
    ADD COLUMN IF NOT EXISTS ai_overage_enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS ai_overage_monthly_limit_micro bigint
        CHECK (ai_overage_monthly_limit_micro IS NULL OR ai_overage_monthly_limit_micro > 0);
