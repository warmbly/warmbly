-- Fail the deploy rather than queue warmup traffic behind a lock we cannot get.
SET LOCAL lock_timeout = '10s';

-- Which campaign step (and A/B arm) a content_source='campaign' thread carries.
-- Nullable and unconstrained: a deleted step leaves its history unattributed.
ALTER TABLE public.warmup_tokens
    ADD COLUMN sequence_id uuid,
    ADD COLUMN variant_id uuid;
