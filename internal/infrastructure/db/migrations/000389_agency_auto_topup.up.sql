-- An agency's automatic top-up: when its pool runs low, a send pack is bought
-- on its saved card, up to a number of packs a period. Off by default.
-- Expand only; the previous release ignores all of it.
ALTER TABLE agencies
    ADD COLUMN IF NOT EXISTS auto_topup_enabled boolean NOT NULL DEFAULT false,
    -- A pack is bought once this many sends or fewer are left in the period.
    ADD COLUMN IF NOT EXISTS auto_topup_threshold integer NOT NULL DEFAULT 10000 CHECK (auto_topup_threshold >= 0),
    ADD COLUMN IF NOT EXISTS auto_topup_max_packs integer NOT NULL DEFAULT 2 CHECK (auto_topup_max_packs BETWEEN 1 AND 20),
    -- Why it turned itself off, until it is turned on again.
    ADD COLUMN IF NOT EXISTS auto_topup_failure text;

ALTER TABLE agency_pool_topups
    ADD COLUMN IF NOT EXISTS automatic boolean NOT NULL DEFAULT false;
