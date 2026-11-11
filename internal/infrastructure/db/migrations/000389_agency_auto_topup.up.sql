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

-- One row per automatic top-up, written before its card is charged, so a
-- charge is always found again and never made twice.
CREATE TABLE IF NOT EXISTS agency_auto_topups (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agency_id         uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    kind              text NOT NULL CHECK (kind IN ('sends', 'ai')),
    period_start      timestamptz NOT NULL,
    amount_cents      bigint NOT NULL CHECK (amount_cents > 0),
    sends             integer NOT NULL DEFAULT 0 CHECK (sends >= 0),
    status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'declined', 'cancelled')),
    stripe_invoice_id text,
    payment_ref       text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz
);

-- At most one in flight for an agency's sends and one for its AI balance.
CREATE UNIQUE INDEX IF NOT EXISTS agency_auto_topups_pending_key ON agency_auto_topups (agency_id, kind) WHERE status = 'pending';
CREATE UNIQUE INDEX IF NOT EXISTS agency_auto_topups_invoice_key ON agency_auto_topups (stripe_invoice_id) WHERE stripe_invoice_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agency_auto_topups_period ON agency_auto_topups (agency_id, kind, period_start);
