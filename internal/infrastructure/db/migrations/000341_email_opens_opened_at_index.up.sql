-- Alone in its file for CONCURRENTLY. Serves the engagement-log prune, which deletes by age in batches.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_email_opens_opened_at
    ON public.email_opens (opened_at);
