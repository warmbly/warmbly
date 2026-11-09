-- A grant issued by a workspace's own client cannot be refreshed once the client is gone.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM email_accounts_oauth WHERE oauth_app_id IS NOT NULL) THEN
        RAISE EXCEPTION 'mailboxes are signed in through a workspace OAuth app; remove them or sign them in again before downgrading';
    END IF;
END $$;

DROP TABLE IF EXISTS mailbox_connect_links;
DROP INDEX IF EXISTS idx_email_accounts_oauth_app;
ALTER TABLE email_accounts_oauth DROP COLUMN IF EXISTS oauth_app_id;
DROP TABLE IF EXISTS organization_oauth_apps;
