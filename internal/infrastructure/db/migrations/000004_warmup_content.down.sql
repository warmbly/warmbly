DROP INDEX IF EXISTS public.idx_warmup_spam_reports_cohort;
DROP INDEX IF EXISTS public.idx_warmup_tokens_cohort;

ALTER TABLE IF EXISTS public.warmup_spam_reports DROP COLUMN IF EXISTS content_source;
ALTER TABLE IF EXISTS public.warmup_tokens DROP COLUMN IF EXISTS conversation_id;
ALTER TABLE IF EXISTS public.warmup_tokens DROP COLUMN IF EXISTS content_source;

DROP TABLE IF EXISTS public.admin_settings;
DROP TABLE IF EXISTS public.warmup_generation_jobs;
DROP TABLE IF EXISTS public.warmup_conversations;
