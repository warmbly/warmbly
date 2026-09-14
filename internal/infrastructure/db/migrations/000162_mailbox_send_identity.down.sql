ALTER TABLE public.email_accounts
    DROP CONSTRAINT IF EXISTS email_accounts_signature_source_check;

ALTER TABLE public.email_accounts
    DROP COLUMN IF EXISTS signature_imported_at,
    DROP COLUMN IF EXISTS signature_source,
    DROP COLUMN IF EXISTS send_as_synced_at,
    DROP COLUMN IF EXISTS send_as_email,
    DROP COLUMN IF EXISTS send_as;
