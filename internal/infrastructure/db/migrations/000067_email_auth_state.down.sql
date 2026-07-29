DROP INDEX IF EXISTS idx_email_accounts_auth_checked_at;

ALTER TABLE email_accounts
    DROP COLUMN IF EXISTS auth_state,
    DROP COLUMN IF EXISTS auth_spf,
    DROP COLUMN IF EXISTS auth_dkim,
    DROP COLUMN IF EXISTS auth_dmarc,
    DROP COLUMN IF EXISTS auth_dmarc_policy,
    DROP COLUMN IF EXISTS auth_reason,
    DROP COLUMN IF EXISTS auth_checked_at;
