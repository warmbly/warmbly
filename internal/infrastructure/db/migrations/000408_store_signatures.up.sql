-- The signature an order line chose for a purchased mailbox, rendered; NULL is none chosen.
-- signature_set_at stamps Warmbly's copy, signature_provider_at the provider's, so a retry never overwrites a later edit.
ALTER TABLE public.store_mailboxes
    ADD COLUMN IF NOT EXISTS signature_html text,
    ADD COLUMN IF NOT EXISTS signature_plain text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS signature_set_at timestamptz,
    ADD COLUMN IF NOT EXISTS signature_provider_at timestamptz,
    ADD COLUMN IF NOT EXISTS signature_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS signature_attempt_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_store_mailboxes_signature_pending ON public.store_mailboxes (signature_attempt_at NULLS FIRST)
    WHERE signature_html IS NOT NULL AND email_account_id IS NOT NULL AND (signature_set_at IS NULL OR signature_provider_at IS NULL);
