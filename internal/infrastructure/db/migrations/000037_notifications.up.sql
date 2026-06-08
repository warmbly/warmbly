-- Per-user notification preferences (singleton jsonb on users; default-merged in
-- Go so a {} column still yields a complete struct).
ALTER TABLE users ADD COLUMN notification_preferences JSONB NOT NULL DEFAULT '{}'::jsonb;

-- The in-app notification feed (the bell). Realtime delivery is best-effort, so
-- the feed table is the source of truth for unread state.
CREATE TABLE notifications (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    organization_id UUID REFERENCES organizations (id) ON DELETE CASCADE,
    category        TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    link            TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_user_unread ON notifications (user_id, created_at DESC) WHERE read_at IS NULL;
CREATE INDEX idx_notifications_user_recent ON notifications (user_id, created_at DESC);
