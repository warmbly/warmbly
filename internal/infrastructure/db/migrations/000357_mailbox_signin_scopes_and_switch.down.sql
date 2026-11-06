ALTER TABLE public.email_accounts DROP CONSTRAINT IF EXISTS email_accounts_signin_switch_state_check;
ALTER TABLE public.email_accounts
    DROP COLUMN IF EXISTS signin_switch_automatic,
    DROP COLUMN IF EXISTS signin_switch_message,
    DROP COLUMN IF EXISTS signin_switch_at,
    DROP COLUMN IF EXISTS signin_switch_state;
ALTER TABLE public.email_accounts_oauth DROP COLUMN IF EXISTS granted_scopes;
