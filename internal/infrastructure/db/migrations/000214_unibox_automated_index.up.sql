-- Alone in its file for CONCURRENTLY. The inbox asks whether a conversation
-- has a machine message; only those rows are indexed, so it stays small.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_unibox_emails_automated
    ON public.unibox_emails (thread_id)
    WHERE automated;
