-- Alone in its file for CONCURRENTLY. The queued campaign actions the cancellation sweep reads.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_social_actions_queued
    ON public.social_actions (created_at)
    WHERE status = 'queued';
