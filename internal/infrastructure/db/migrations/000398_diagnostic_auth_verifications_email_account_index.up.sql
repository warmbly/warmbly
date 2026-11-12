-- Alone in its file for CONCURRENTLY. Lets a mailbox delete cascade here by index instead of scanning the table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_diagnostic_auth_verifications_email_account
    ON public.diagnostic_auth_verifications (email_account_id);
