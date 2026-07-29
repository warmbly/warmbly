-- Email digest state for the notification email channel. Rows queue as
-- pending with a due time derived from the user's digest cadence; a flush
-- loop bundles due rows into one email per user (or one shared email per
-- org group), and reading a notification in-app cancels its pending email.
ALTER TABLE notifications
    ADD COLUMN group_key TEXT,
    ADD COLUMN email_state TEXT NOT NULL DEFAULT 'none'
        CONSTRAINT notifications_email_state_check
        CHECK (email_state IN ('none', 'pending', 'sending', 'sent', 'skipped')),
    ADD COLUMN email_due_at TIMESTAMPTZ,
    ADD COLUMN email_attempts INT NOT NULL DEFAULT 0;

CREATE INDEX idx_notifications_email_pending ON notifications (email_due_at)
    WHERE email_state IN ('pending', 'sending');
