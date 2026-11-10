-- What an operator sets on one agency: a plan held without Stripe, a pool and
-- AI allowance of its own, and credits. Expand only; the previous release ignores all of it.
ALTER TABLE agencies
    ADD COLUMN IF NOT EXISTS managed_plan_id uuid REFERENCES plans (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS managed_at timestamptz,
    ADD COLUMN IF NOT EXISTS managed_by uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS managed_reason text,
    -- NULL with managed_at set is a grant with no end.
    ADD COLUMN IF NOT EXISTS managed_until timestamptz,
    -- Stands in for the plan's monthly_send_pool; NULL is the plan's own.
    ADD COLUMN IF NOT EXISTS pool_override integer CHECK (pool_override IS NULL OR pool_override > 0),
    -- Stands in for the plan's agency_ai_allowance_micro; NULL is the plan's own.
    ADD COLUMN IF NOT EXISTS ai_allowance_override_micro bigint CHECK (ai_allowance_override_micro IS NULL OR ai_allowance_override_micro >= 0),
    ADD COLUMN IF NOT EXISTS override_reason text,
    ADD COLUMN IF NOT EXISTS override_by uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS override_at timestamptz;

-- Sends an operator added to a period have no payment; these say who and why.
ALTER TABLE agency_pool_topups
    ADD COLUMN IF NOT EXISTS granted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS reason text;
