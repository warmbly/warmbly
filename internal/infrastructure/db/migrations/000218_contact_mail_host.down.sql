ALTER TABLE contacts
    DROP CONSTRAINT IF EXISTS contacts_mail_host_check,
    DROP COLUMN IF EXISTS mail_host;
