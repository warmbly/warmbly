-- Pre-send email verification columns on contacts.
--
-- Suppression today is reactive: we only stop sending to an address *after* it
-- hard-bounces or complains, so every bad address costs a real bounce against
-- our sending reputation. These columns let the control plane verify an address
-- (syntax -> MX -> SMTP RCPT probe -> catch-all detection) before any worker
-- sends to it, turning a future hard bounce into a zero-cost pre-send drop.
--
-- status values mirror emailverify.Status: 'valid' | 'risky' | 'invalid' |
-- 'unknown'. New contacts default to 'unknown' so the verification scheduler
-- picks them up; the pre-send gate only ever drops 'invalid'.

ALTER TABLE public.contacts
    ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS verification_reason text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS is_catch_all boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS verification_checked_at timestamptz;

-- Partial index over the addresses the batch scheduler scans for: anything not
-- yet conclusively checked (status = 'unknown' AND never verified). Keeps
-- ListUnverifiedContacts a cheap index scan instead of a full table sweep.
CREATE INDEX IF NOT EXISTS idx_contacts_verification_pending
    ON public.contacts (verification_checked_at)
    WHERE verification_status = 'unknown' AND verification_checked_at IS NULL;

-- Supports the pre-send gate's "is this contact invalid?" lookups.
CREATE INDEX IF NOT EXISTS idx_contacts_verification_status
    ON public.contacts (verification_status);
