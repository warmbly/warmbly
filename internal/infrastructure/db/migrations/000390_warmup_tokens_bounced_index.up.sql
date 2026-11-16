-- Alone in its file for CONCURRENTLY. Serves the partner draw's read of recipients whose address bounced pool warmup.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warmup_tokens_recipient_bounced
    ON public.warmup_tokens (recipient_account_id, bounced_at)
    WHERE bounced_at IS NOT NULL;
