-- The OAuth scopes Google granted a mailbox's stored token; NULL is a token stored before they were recorded.
ALTER TABLE public.email_accounts_oauth ADD COLUMN granted_scopes text[];

-- Where the last request for an inbox vendor to sign in again as the mailbox stands.
-- signin_switch_at moves with every change, so a retry is measured from how the last one ended.
ALTER TABLE public.email_accounts
    ADD COLUMN signin_switch_state text,
    ADD COLUMN signin_switch_at timestamptz,
    ADD COLUMN signin_switch_message text NOT NULL DEFAULT '',
    ADD COLUMN signin_switch_automatic boolean NOT NULL DEFAULT false;

-- NOT VALID skips the scan: every existing row is NULL, and new writes are still checked.
ALTER TABLE public.email_accounts
    ADD CONSTRAINT email_accounts_signin_switch_state_check
        CHECK (signin_switch_state IS NULL OR signin_switch_state IN ('queued', 'trusting', 'signing_in', 'connected', 'failed')) NOT VALID;
