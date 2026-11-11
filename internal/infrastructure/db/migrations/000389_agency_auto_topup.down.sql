ALTER TABLE agency_pool_topups
    DROP COLUMN IF EXISTS automatic;

ALTER TABLE agencies
    DROP COLUMN IF EXISTS auto_topup_failure,
    DROP COLUMN IF EXISTS auto_topup_max_packs,
    DROP COLUMN IF EXISTS auto_topup_threshold,
    DROP COLUMN IF EXISTS auto_topup_enabled;
