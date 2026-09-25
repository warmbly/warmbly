-- A trusted verdict that no person wrote a message. The verdict row keeps it so
-- a message re-synced later inherits it; unibox_emails carries it so the inbox
-- can leave out conversations made only of machine mail.
ALTER TABLE public.inbox_tag_results
    ADD COLUMN IF NOT EXISTS automated boolean NOT NULL DEFAULT false;

ALTER TABLE public.unibox_emails
    ADD COLUMN IF NOT EXISTS automated boolean NOT NULL DEFAULT false;
