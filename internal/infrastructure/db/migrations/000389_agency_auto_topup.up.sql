-- An agency's automatic top-up: when its pool runs low, a send pack is bought
-- on its saved card, up to a number of packs a period. Off by default.
-- Expand only; the previous release ignores all of it.
ALTER TABLE agencies
    ADD COLUMN IF NOT EXISTS auto_topup_enabled boolean NOT NULL DEFAULT false,
    -- A pack is bought once this many sends or fewer are left in the period.
    ADD COLUMN IF NOT EXISTS auto_topup_threshold integer NOT NULL DEFAULT 10000 CHECK (auto_topup_threshold >= 0),
    ADD COLUMN IF NOT EXISTS auto_topup_max_packs integer NOT NULL DEFAULT 2 CHECK (auto_topup_max_packs BETWEEN 1 AND 20),
    -- Why it turned itself off, until it is turned on again.
    ADD COLUMN IF NOT EXISTS auto_topup_failure text,
    -- The same for the shared AI balance: under the threshold, the amount is added, up to the limit a period.
    ADD COLUMN IF NOT EXISTS ai_auto_topup_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS ai_auto_topup_threshold_micro bigint NOT NULL DEFAULT 5000000 CHECK (ai_auto_topup_threshold_micro >= 0),
    ADD COLUMN IF NOT EXISTS ai_auto_topup_amount_micro bigint NOT NULL DEFAULT 25000000 CHECK (ai_auto_topup_amount_micro > 0),
    ADD COLUMN IF NOT EXISTS ai_auto_topup_limit_micro bigint NOT NULL DEFAULT 100000000 CHECK (ai_auto_topup_limit_micro > 0),
    ADD COLUMN IF NOT EXISTS ai_auto_topup_failure text;

ALTER TABLE agency_pool_topups
    ADD COLUMN IF NOT EXISTS automatic boolean NOT NULL DEFAULT false;
