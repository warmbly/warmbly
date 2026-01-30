-- Drop indexes
DROP INDEX IF EXISTS idx_tasks_scheduled_date;
DROP INDEX IF EXISTS idx_tasks_account_completed;
DROP INDEX IF EXISTS idx_tasks_account_scheduled;

-- Remove columns
ALTER TABLE email_tasks DROP COLUMN IF EXISTS encrypted;
ALTER TABLE tasks DROP COLUMN IF EXISTS cloud_task_name;
ALTER TABLE tasks DROP COLUMN IF EXISTS completed_at;
ALTER TABLE tasks DROP COLUMN IF EXISTS scheduled_at;
