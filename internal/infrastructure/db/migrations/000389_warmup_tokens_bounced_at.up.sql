-- Fail the deploy rather than queue warmup traffic behind a lock we cannot get.
SET LOCAL lock_timeout = '10s';

-- When the sender's mailbox got a permanent "no such recipient" DSN for this send.
-- Nullable and unbackfilled: older releases neither read nor write it.
ALTER TABLE public.warmup_tokens
    ADD COLUMN bounced_at timestamptz;
