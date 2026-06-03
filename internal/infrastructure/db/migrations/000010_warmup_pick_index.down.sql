DROP INDEX IF EXISTS public.idx_warmup_conversations_pick;

CREATE INDEX idx_warmup_conversations_pick ON public.warmup_conversations
    USING btree (pool_type, segment, status);
