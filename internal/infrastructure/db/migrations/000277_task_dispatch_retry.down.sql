DROP INDEX IF EXISTS tasks_pending_dispatch_due;
ALTER TABLE tasks
    DROP COLUMN dispatch_failure_at,
    DROP COLUMN dispatch_failure_since,
    DROP COLUMN dispatch_retry_at;
