-- The worker a mailbox last left and when, so that worker may finish in-flight work for it during a bounded grace.
-- Recorded by a trigger so every path that moves a mailbox, including an older release's, records it.
ALTER TABLE email_accounts
    ADD COLUMN IF NOT EXISTS previous_worker_id uuid,
    ADD COLUMN IF NOT EXISTS worker_released_at timestamptz;

CREATE OR REPLACE FUNCTION email_accounts_record_worker_release() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.worker_id IS NOT NULL AND NEW.worker_id IS DISTINCT FROM OLD.worker_id THEN
        NEW.previous_worker_id := OLD.worker_id;
        NEW.worker_released_at := now();
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS email_accounts_worker_release ON email_accounts;
CREATE TRIGGER email_accounts_worker_release
    BEFORE UPDATE OF worker_id ON email_accounts
    FOR EACH ROW EXECUTE FUNCTION email_accounts_record_worker_release();

-- Empty at creation: every row is NULL until a mailbox next moves.
CREATE INDEX IF NOT EXISTS idx_email_accounts_previous_worker
    ON email_accounts (previous_worker_id, worker_released_at) WHERE previous_worker_id IS NOT NULL;
