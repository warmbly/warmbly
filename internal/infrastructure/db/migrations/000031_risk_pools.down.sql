DROP INDEX IF EXISTS idx_email_accounts_risk_band;
DROP INDEX IF EXISTS idx_workers_risk_pool;

ALTER TABLE email_accounts
    DROP COLUMN IF EXISTS risk_evaluated_at,
    DROP COLUMN IF EXISTS risk_band;

ALTER TABLE workers
    DROP COLUMN IF EXISTS risk_pool;

DROP TYPE IF EXISTS email_risk_band;
DROP TYPE IF EXISTS worker_risk_pool;
