DROP INDEX IF EXISTS idx_notifications_email_pending;
ALTER TABLE notifications
    DROP COLUMN IF EXISTS group_key,
    DROP COLUMN IF EXISTS email_state,
    DROP COLUMN IF EXISTS email_due_at,
    DROP COLUMN IF EXISTS email_attempts;
