-- The Unibox conversation a forward was sent from. Only Warmbly files it there;
-- on the wire the forward stays a new conversation (no thread, no In-Reply-To).
ALTER TABLE email_tasks ADD COLUMN IF NOT EXISTS forward_thread_id text;

-- A sent forward filed into the conversation it was forwarded from. It is shown
-- there but is not part of that conversation for the recipient, so reply
-- threading skips it.
ALTER TABLE unibox_emails ADD COLUMN IF NOT EXISTS filed_forward boolean NOT NULL DEFAULT false;
