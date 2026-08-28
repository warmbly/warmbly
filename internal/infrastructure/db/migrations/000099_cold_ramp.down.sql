ALTER TABLE public.email_accounts
    DROP COLUMN IF EXISTS cold_ramp_started_at;
