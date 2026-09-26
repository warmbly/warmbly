-- task_type keeps 'placement': Postgres cannot drop an enum value.
DROP INDEX IF EXISTS idx_placement_results_task;
DROP INDEX IF EXISTS idx_placement_results_test_address;

-- Results of tests that never had a local seed cannot go back to a NOT NULL
-- seed column.
DELETE FROM placement_results WHERE seed_account_id IS NULL;
ALTER TABLE placement_results
    DROP CONSTRAINT placement_results_seed_account_id_fkey,
    ADD CONSTRAINT placement_results_seed_account_id_fkey
        FOREIGN KEY (seed_account_id) REFERENCES email_accounts (id) ON DELETE CASCADE;
UPDATE placement_results SET folder = 'other', raw_flags = 'timeout' WHERE folder = 'missing';
UPDATE placement_results SET folder = 'other' WHERE folder IN ('failed', 'cancelled');

ALTER TABLE placement_results
    DROP CONSTRAINT IF EXISTS placement_results_folder_check,
    DROP COLUMN IF EXISTS seed_address,
    DROP COLUMN IF EXISTS remote_seed_id,
    DROP COLUMN IF EXISTS task_id,
    DROP COLUMN IF EXISTS message_id,
    DROP COLUMN IF EXISTS scheduled_at,
    DROP COLUMN IF EXISTS sent_at,
    DROP COLUMN IF EXISTS remote_synced_at,
    DROP COLUMN IF EXISTS error,
    ALTER COLUMN seed_account_id SET NOT NULL,
    ADD CONSTRAINT placement_results_folder_check
        CHECK (folder IN ('inbox', 'promotions', 'spam', 'other', 'pending'));

ALTER TABLE placement_monitors DROP CONSTRAINT IF EXISTS placement_monitors_last_test_fkey;

DROP INDEX IF EXISTS idx_placement_tests_running;
DROP INDEX IF EXISTS idx_placement_tests_campaign;
DROP INDEX IF EXISTS idx_placement_tests_group;

DELETE FROM placement_tests WHERE sender_account_id IS NULL;
ALTER TABLE placement_tests
    DROP CONSTRAINT placement_tests_sender_account_id_fkey,
    ADD CONSTRAINT placement_tests_sender_account_id_fkey
        FOREIGN KEY (sender_account_id) REFERENCES email_accounts (id) ON DELETE CASCADE;
UPDATE placement_tests SET status = 'pending' WHERE status = 'running';
UPDATE placement_tests SET status = 'completed' WHERE status IN ('cancelled', 'failed');

ALTER TABLE placement_tests
    DROP CONSTRAINT IF EXISTS placement_tests_status_check,
    DROP CONSTRAINT IF EXISTS placement_tests_origin_check,
    DROP CONSTRAINT IF EXISTS placement_tests_panel_check,
    ALTER COLUMN status SET DEFAULT 'pending',
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS campaign_id,
    DROP COLUMN IF EXISTS sequence_id,
    DROP COLUMN IF EXISTS contact_id,
    DROP COLUMN IF EXISTS monitor_id,
    DROP COLUMN IF EXISTS open_tracking,
    DROP COLUMN IF EXISTS link_tracking,
    DROP COLUMN IF EXISTS compare_group_id,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS panel,
    DROP COLUMN IF EXISTS sender_email,
    DROP COLUMN IF EXISTS remote_instance_id,
    DROP COLUMN IF EXISTS remote_test_id,
    DROP COLUMN IF EXISTS error,
    ALTER COLUMN sender_account_id SET NOT NULL,
    ADD COLUMN token text NOT NULL UNIQUE DEFAULT gen_random_uuid()::text;

DROP TABLE IF EXISTS placement_monitors;

ALTER TABLE email_accounts ADD COLUMN is_seed boolean NOT NULL DEFAULT false;
UPDATE email_accounts SET is_seed = true WHERE seed_scope = 'instance';
DROP INDEX IF EXISTS idx_email_accounts_seed_scope;
ALTER TABLE email_accounts
    DROP CONSTRAINT IF EXISTS email_accounts_seed_scope_check,
    DROP COLUMN IF EXISTS seed_scope;
CREATE INDEX idx_email_accounts_is_seed ON email_accounts (is_seed) WHERE is_seed = true;
