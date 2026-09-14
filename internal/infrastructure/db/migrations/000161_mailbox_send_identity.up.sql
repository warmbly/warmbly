-- A Gmail mailbox knows two things Warmbly was making the customer retype: the
-- signature they already wrote in Gmail, and the addresses Google has verified
-- them to send as. Both live behind gmail.settings.basic, which the consent
-- screen already asks for.
--
-- send_as is the list as the provider last reported it: address, display name,
-- and whether Google treats it as the primary or the default identity. It is a
-- read-then-use blob that nothing filters in SQL, so it is jsonb rather than a
-- table, and it is validated at the app boundary on write (models.SendAsIdentity).
--
-- send_as_email is the one the customer picked, and it is a plain column
-- because the send path reads it on every dispatch. Empty means the mailbox's
-- own address, which is what every existing mailbox keeps.
--
-- signature_source records where the stored signature came from, so the
-- dashboard can say "imported from Gmail" and an import can be repeated
-- without quietly overwriting something a customer typed by hand. Note that
-- signature_sync is unrelated and always has been: it controls whether the
-- signature is appended to outgoing mail.
ALTER TABLE public.email_accounts
    ADD COLUMN IF NOT EXISTS send_as jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS send_as_email text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS send_as_synced_at timestamptz,
    ADD COLUMN IF NOT EXISTS signature_source text NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS signature_imported_at timestamptz;

-- The discriminator gets a CHECK so a typo in app code cannot invent a third
-- source that every reader then has to guess at.
ALTER TABLE public.email_accounts
    DROP CONSTRAINT IF EXISTS email_accounts_signature_source_check;

ALTER TABLE public.email_accounts
    ADD CONSTRAINT email_accounts_signature_source_check
    CHECK (signature_source IN ('manual', 'provider'));
