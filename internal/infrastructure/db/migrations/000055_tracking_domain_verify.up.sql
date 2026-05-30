-- Custom tracking-domain verification. A mailbox can point a subdomain
-- (CNAME) at the shared tracking host; we resolve the CNAME and only
-- treat the domain as live once it targets us.
ALTER TABLE email_accounts
    ADD COLUMN IF NOT EXISTS tracking_domain_verified BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS tracking_domain_verified_at TIMESTAMPTZ;
