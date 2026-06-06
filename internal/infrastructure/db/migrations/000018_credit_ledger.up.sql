-- AI writing-assistant credit system. Two tables plus a per-plan monthly grant.
--
-- credit_ledger is the authoritative per-organization balance (one row per org).
-- Consumption is a single atomic conditional UPDATE
-- (balance = balance - $n WHERE balance >= $n RETURNING ...) so concurrent
-- generation requests can never drive the balance negative — billing
-- correctness lives in the database, not in application-level read-modify-write.
--
-- credit_ledger_transactions is the append-only audit trail. idempotency_key is
-- nullable+unique so a retried POST /generation/write (same Idempotency-Key)
-- can be detected and not double-charged.

CREATE TABLE IF NOT EXISTS credit_ledger (
    org_id          uuid PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
    balance         integer NOT NULL DEFAULT 0 CHECK (balance >= 0),
    month_reset_at  timestamptz NOT NULL DEFAULT now(),
    total_purchased integer NOT NULL DEFAULT 0 CHECK (total_purchased >= 0),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS credit_ledger_transactions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    amount          integer NOT NULL,            -- negative = consumption, positive = grant/purchase
    reason          text NOT NULL,
    model_used      text NOT NULL DEFAULT '',
    tokens_used     integer NOT NULL DEFAULT 0,
    balance_after   integer NOT NULL,
    idempotency_key text,                         -- nullable; unique when present
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_credit_txns_org ON credit_ledger_transactions (org_id, created_at DESC);

-- Partial unique index: at most one transaction per idempotency_key, but NULL
-- keys (grants, resets, non-idempotent calls) are unconstrained.
CREATE UNIQUE INDEX IF NOT EXISTS uq_credit_txns_idempotency
    ON credit_ledger_transactions (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Per-plan monthly credit grant. Additive with a safe default so existing
-- plans (and the free trial) start at 0 until seeded.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS monthly_credits integer NOT NULL DEFAULT 0;
