DROP INDEX IF EXISTS public.idx_warmup_generation_jobs_one_scheduled_inflight;
DROP INDEX IF EXISTS public.idx_warmup_spam_reports_message_placement;
DROP INDEX IF EXISTS public.idx_email_tasks_in_reply_to;
DROP INDEX IF EXISTS public.idx_warmup_tokens_conversation_created;

ALTER TABLE public.warmup_conversations
    DROP COLUMN IF EXISTS reply_eligible;

ALTER TABLE public.warmup_tokens
    DROP CONSTRAINT IF EXISTS warmup_tokens_conversation_turn_check,
    DROP COLUMN IF EXISTS conversation_turn;

DROP INDEX IF EXISTS public.idx_warmup_conversations_pick;

CREATE INDEX idx_warmup_conversations_pick ON public.warmup_conversations
    USING btree (segment) WHERE (status = 'active'::text);
