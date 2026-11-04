-- Alone in its file for CONCURRENTLY. Serves the engagement-log prune, which deletes by age in batches.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_email_link_clicks_clicked_at
    ON public.email_link_clicks (clicked_at);
