-- Postgres cannot drop a single enum value, so 'skipped_domain_auth' stays on
-- task_status. Any task parked in it is returned to the generic warmup skip
-- first, so nothing is left in a status the application no longer knows.
UPDATE tasks SET status = 'skipped_warmup_protected' WHERE status = 'skipped_domain_auth';

DROP INDEX IF EXISTS idx_email_accounts_auth_failing;

ALTER TABLE email_accounts
    DROP COLUMN IF EXISTS auth_failing_since;
