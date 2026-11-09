-- Alone in its file for CONCURRENTLY. Serves the per-app mailbox count and the reference check on app removal.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_email_accounts_oauth_app
    ON email_accounts_oauth (oauth_app_id) WHERE oauth_app_id IS NOT NULL;
