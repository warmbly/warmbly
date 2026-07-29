-- Keep content selection and unattended batch generation efficient at scale.

ALTER TABLE public.warmup_tokens
    ADD COLUMN conversation_turn integer DEFAULT 0 NOT NULL,
    ADD CONSTRAINT warmup_tokens_conversation_turn_check CHECK (conversation_turn >= 0 AND conversation_turn <= 5);

ALTER TABLE public.warmup_conversations
    ADD COLUMN reply_eligible boolean DEFAULT true NOT NULL;

-- Earlier AI rows stored interchangeable suggestions rather than ordered
-- turns. Retire them so the controller replaces the bank with coherent plans.
UPDATE public.warmup_conversations
SET status = 'archived', reply_eligible = false, updated_at = NOW()
WHERE source = 'ai' AND status = 'active';

DROP INDEX IF EXISTS public.idx_warmup_conversations_pick;

CREATE INDEX idx_warmup_conversations_pick ON public.warmup_conversations
    USING btree (segment, usage_count, id) WHERE (status = 'active'::text);

CREATE INDEX idx_warmup_tokens_conversation_created
    ON public.warmup_tokens (conversation_id, created_at DESC)
    WHERE conversation_id IS NOT NULL;

CREATE INDEX idx_email_tasks_in_reply_to
    ON public.email_tasks USING gin (in_reply_to);

CREATE INDEX idx_warmup_spam_reports_message_placement
    ON public.warmup_spam_reports (message_id)
    WHERE report_type = 'spam_placement';

-- Older deployments may already have overlapping scheduled jobs. Preserve the
-- newest one and close the rest before adding the invariant.
WITH ranked AS (
    SELECT id,
           row_number() OVER (PARTITION BY pool_type, segment ORDER BY created_at DESC) AS position
    FROM public.warmup_generation_jobs
    WHERE trigger = 'schedule' AND status IN ('pending', 'running')
)
UPDATE public.warmup_generation_jobs AS jobs
SET status = 'failed',
    error = 'superseded during scheduled generation migration',
    finished_at = NOW(),
    updated_at = NOW()
FROM ranked
WHERE jobs.id = ranked.id AND ranked.position > 1;

CREATE UNIQUE INDEX idx_warmup_generation_jobs_one_scheduled_inflight
    ON public.warmup_generation_jobs (pool_type, segment)
    WHERE trigger = 'schedule' AND status IN ('pending', 'running');
