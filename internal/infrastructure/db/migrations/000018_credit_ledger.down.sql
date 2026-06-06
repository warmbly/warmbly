ALTER TABLE plans DROP COLUMN IF EXISTS monthly_credits;

DROP INDEX IF EXISTS uq_credit_txns_idempotency;
DROP INDEX IF EXISTS idx_credit_txns_org;
DROP TABLE IF EXISTS credit_ledger_transactions;
DROP TABLE IF EXISTS credit_ledger;
