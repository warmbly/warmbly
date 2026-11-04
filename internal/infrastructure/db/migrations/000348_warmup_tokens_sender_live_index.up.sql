-- Alone in its file for CONCURRENTLY. Serves the warmup mail retention listing for each sender's own copies.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warmup_tokens_sender_live
    ON public.warmup_tokens (sender_account_id, created_at)
    WHERE sent_retired_at IS NULL AND sent_message_id <> '';
