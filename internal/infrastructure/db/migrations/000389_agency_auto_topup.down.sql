ALTER TABLE agency_pool_topups
    DROP COLUMN IF EXISTS automatic;

ALTER TABLE agencies
    DROP COLUMN IF EXISTS ai_auto_topup_failure,
    DROP COLUMN IF EXISTS ai_auto_topup_limit_micro,
    DROP COLUMN IF EXISTS ai_auto_topup_amount_micro,
    DROP COLUMN IF EXISTS ai_auto_topup_threshold_micro,
    DROP COLUMN IF EXISTS ai_auto_topup_enabled,
    DROP COLUMN IF EXISTS auto_topup_failure,
    DROP COLUMN IF EXISTS auto_topup_max_packs,
    DROP COLUMN IF EXISTS auto_topup_threshold,
    DROP COLUMN IF EXISTS auto_topup_enabled;
