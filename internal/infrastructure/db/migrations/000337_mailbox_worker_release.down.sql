DROP INDEX IF EXISTS idx_email_accounts_previous_worker;
DROP TRIGGER IF EXISTS email_accounts_worker_release ON email_accounts;
DROP FUNCTION IF EXISTS email_accounts_record_worker_release();
ALTER TABLE email_accounts
    DROP COLUMN IF EXISTS worker_released_at,
    DROP COLUMN IF EXISTS previous_worker_id;
