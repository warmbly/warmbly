ALTER TABLE email_accounts
    DROP COLUMN IF EXISTS tracking_domain_verified,
    DROP COLUMN IF EXISTS tracking_domain_verified_at;
