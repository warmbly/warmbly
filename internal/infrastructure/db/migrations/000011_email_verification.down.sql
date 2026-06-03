DROP INDEX IF EXISTS public.idx_contacts_verification_status;
DROP INDEX IF EXISTS public.idx_contacts_verification_pending;

ALTER TABLE public.contacts
    DROP COLUMN IF EXISTS verification_checked_at,
    DROP COLUMN IF EXISTS is_catch_all,
    DROP COLUMN IF EXISTS verification_reason,
    DROP COLUMN IF EXISTS verification_status;
