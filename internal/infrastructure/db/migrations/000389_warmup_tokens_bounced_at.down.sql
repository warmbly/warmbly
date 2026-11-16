SET LOCAL lock_timeout = '10s';

ALTER TABLE public.warmup_tokens
    DROP COLUMN IF EXISTS bounced_at;
