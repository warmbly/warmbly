DROP INDEX IF EXISTS public.idx_store_mailboxes_signature_pending;
ALTER TABLE public.store_mailboxes
    DROP COLUMN IF EXISTS signature_attempt_at,
    DROP COLUMN IF EXISTS signature_attempts,
    DROP COLUMN IF EXISTS signature_provider_at,
    DROP COLUMN IF EXISTS signature_set_at,
    DROP COLUMN IF EXISTS signature_plain,
    DROP COLUMN IF EXISTS signature_html;
