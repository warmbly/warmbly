DROP INDEX IF EXISTS idx_unibox_emails_body_search;
ALTER TABLE unibox_emails DROP COLUMN IF EXISTS body_text;
