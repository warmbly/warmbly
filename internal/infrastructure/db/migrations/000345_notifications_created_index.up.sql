-- Alone in its file for CONCURRENTLY. Serves the notification prune.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_notifications_created
    ON public.notifications (created_at);
