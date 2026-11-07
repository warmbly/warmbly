-- Alone in its file for CONCURRENTLY. Serves a campaign's warmup placement and the per-recipient copy gate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warmup_tokens_sequence
    ON public.warmup_tokens (sequence_id, created_at)
    WHERE sequence_id IS NOT NULL;
