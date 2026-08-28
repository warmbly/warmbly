DROP INDEX IF EXISTS public.idx_users_signup_email_normalized;
DROP INDEX IF EXISTS public.idx_users_signup_ip;

ALTER TABLE public.users
    DROP CONSTRAINT IF EXISTS users_signup_email_risk_check;

ALTER TABLE public.users
    DROP COLUMN IF EXISTS signup_email_normalized,
    DROP COLUMN IF EXISTS signup_email_risk,
    DROP COLUMN IF EXISTS signup_user_agent,
    DROP COLUMN IF EXISTS signup_ip;
