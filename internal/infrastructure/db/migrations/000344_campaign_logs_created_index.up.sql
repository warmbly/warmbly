-- Alone in its file for CONCURRENTLY. Serves the campaign activity-feed prune.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_campaign_logs_created
    ON public.campaign_logs (created_at);
