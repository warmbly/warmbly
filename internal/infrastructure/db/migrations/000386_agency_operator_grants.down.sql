ALTER TABLE agency_pool_topups
    DROP COLUMN IF EXISTS reason,
    DROP COLUMN IF EXISTS granted_by;

ALTER TABLE agencies
    DROP COLUMN IF EXISTS override_at,
    DROP COLUMN IF EXISTS override_by,
    DROP COLUMN IF EXISTS override_reason,
    DROP COLUMN IF EXISTS ai_allowance_override_micro,
    DROP COLUMN IF EXISTS pool_override,
    DROP COLUMN IF EXISTS managed_until,
    DROP COLUMN IF EXISTS managed_reason,
    DROP COLUMN IF EXISTS managed_by,
    DROP COLUMN IF EXISTS managed_at,
    DROP COLUMN IF EXISTS managed_plan_id;
