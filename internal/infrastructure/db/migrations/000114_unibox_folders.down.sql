DROP INDEX IF EXISTS idx_unibox_emails_folder;

ALTER TABLE unibox_emails
    DROP CONSTRAINT IF EXISTS unibox_emails_folder_check;

ALTER TABLE unibox_emails
    DROP COLUMN IF EXISTS folder;
