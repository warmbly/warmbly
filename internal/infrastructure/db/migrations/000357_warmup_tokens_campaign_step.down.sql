SET LOCAL lock_timeout = '10s';

ALTER TABLE public.warmup_tokens
    DROP COLUMN IF EXISTS variant_id,
    DROP COLUMN IF EXISTS sequence_id;
