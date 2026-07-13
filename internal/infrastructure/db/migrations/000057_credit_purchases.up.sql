-- Purchased AI credits. The ledger gains a second pool: `balance` remains the
-- monthly plan allowance (reset to plan.monthly_credits each billing cycle),
-- `purchased_balance` holds top-up credits that never expire and survive
-- resets. Consumption drains the monthly pool first, then purchased, in one
-- atomic conditional UPDATE (WHERE balance + purchased_balance >= amount).
--
-- Transactions record the split via purchased_delta (the signed portion of
-- `amount` applied to the purchased pool) plus purchased_balance_after, so the
-- append-only log remains a complete reconstruction of both pools.

ALTER TABLE credit_ledger
    ADD COLUMN IF NOT EXISTS purchased_balance integer NOT NULL DEFAULT 0 CHECK (purchased_balance >= 0);

ALTER TABLE credit_ledger_transactions
    ADD COLUMN IF NOT EXISTS purchased_delta integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS purchased_balance_after integer NOT NULL DEFAULT 0;
