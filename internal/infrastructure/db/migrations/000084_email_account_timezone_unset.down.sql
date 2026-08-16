-- Restore the previous default. The backfill is not reversed: an empty
-- timezone and 'UTC' are indistinguishable once merged, and rewriting every
-- empty row back to 'UTC' would clobber mailboxes deliberately left unset.
ALTER TABLE email_accounts ALTER COLUMN timezone SET DEFAULT 'UTC';
