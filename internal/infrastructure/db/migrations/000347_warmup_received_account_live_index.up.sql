-- Alone in its file for CONCURRENTLY. Serves the warmup mail retention listing, which walks each mailbox's unretired receipts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warmup_received_account_live
    ON public.warmup_received (email_account_id, created_at)
    WHERE retired_at IS NULL;
