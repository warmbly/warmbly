DROP TRIGGER IF EXISTS email_accounts_activated ON email_accounts;
DROP FUNCTION IF EXISTS email_accounts_activated();
ALTER TABLE email_accounts DROP COLUMN IF EXISTS activated_at;
