-- An agency's shared AI balance: a per-period allowance from its plan plus
-- money it bought, spent by client workspaces after their own balance.
-- Expand only; the previous release ignores all of it.

-- What an agency plan refills the shared balance to each period; NULL is none.
ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS agency_ai_allowance_micro bigint CHECK (agency_ai_allowance_micro IS NULL OR agency_ai_allowance_micro >= 0);

UPDATE plans SET agency_ai_allowance_micro = 10000000
WHERE id = '00000000-0000-0000-0000-0000000000a1' AND agency_ai_allowance_micro IS NULL;
UPDATE plans SET agency_ai_allowance_micro = 30000000
WHERE id = '00000000-0000-0000-0000-0000000000a2' AND agency_ai_allowance_micro IS NULL;

-- How much of the agency balance one client may spend per period; NULL is no cap.
ALTER TABLE agency_clients
    ADD COLUMN IF NOT EXISTS monthly_ai_limit_micro bigint CHECK (monthly_ai_limit_micro IS NULL OR monthly_ai_limit_micro >= 0);

-- The part of a workspace charge or refund the agency balance paid or got back.
ALTER TABLE credit_ledger_transactions
    ADD COLUMN IF NOT EXISTS agency_delta_micro bigint;

CREATE TABLE IF NOT EXISTS agency_ai_ledger (
    agency_id             uuid PRIMARY KEY REFERENCES agencies (id) ON DELETE CASCADE,
    -- What is left of the allowance for the period that started at period_start.
    allowance_micro       bigint NOT NULL DEFAULT 0 CHECK (allowance_micro >= 0),
    purchased_micro       bigint NOT NULL DEFAULT 0 CHECK (purchased_micro >= 0),
    total_purchased_micro bigint NOT NULL DEFAULT 0 CHECK (total_purchased_micro >= 0),
    period_start          timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- Append-only. No foreign key on organization_id: a deleted client's spend still counts.
CREATE TABLE IF NOT EXISTS agency_ai_transactions (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agency_id                uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    organization_id          uuid,
    kind                     text NOT NULL CHECK (kind IN ('period_reset', 'spend', 'refund', 'transfer', 'topup', 'topup_refund', 'credit')),
    amount_micro             bigint NOT NULL,
    allowance_delta_micro    bigint NOT NULL DEFAULT 0,
    allowance_after_micro    bigint NOT NULL,
    purchased_after_micro    bigint NOT NULL,
    period_start             timestamptz NOT NULL,
    reason                   text NOT NULL DEFAULT '',
    idempotency_key          text UNIQUE,
    stripe_payment_intent_id text UNIQUE,
    actor_user_id            uuid,
    created_at               timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agency_ai_txn_period ON agency_ai_transactions (agency_id, period_start, organization_id);
CREATE INDEX IF NOT EXISTS idx_agency_ai_txn_created ON agency_ai_transactions (agency_id, created_at DESC);
