ALTER TABLE email_accounts_smtp_imap
    DROP CONSTRAINT IF EXISTS email_accounts_smtp_imap_imap_security_check;

ALTER TABLE email_accounts_smtp_imap
    DROP CONSTRAINT IF EXISTS email_accounts_smtp_imap_smtp_security_check;

ALTER TABLE email_accounts_smtp_imap
    DROP COLUMN IF EXISTS imap_security,
    DROP COLUMN IF EXISTS smtp_security;
