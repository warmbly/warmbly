-- The message a unibox forward carries, rendered when the forward is queued and
-- appended after the note and signature at send time.
ALTER TABLE email_tasks
    ADD COLUMN IF NOT EXISTS forwarded_html text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS forwarded_plain text NOT NULL DEFAULT '';
