-- Alone in its file for CONCURRENTLY. Inbox tagging resolves the campaign a
-- reply answers through the Gmail thread handle the worker reports.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tasks_account_thread
    ON tasks (email_account_id, thread_id)
    WHERE thread_id <> '';
