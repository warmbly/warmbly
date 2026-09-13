-- Reverses 000150. Every exemption is forgotten, so those accounts go back to
-- whatever AUTH_LOGIN_CODE says for everyone else, which is the safe
-- direction: the column only ever removed a check.
DROP INDEX IF EXISTS idx_users_login_code_exempt;

ALTER TABLE public.users
    DROP CONSTRAINT IF EXISTS users_login_code_exempt_reason_check;

ALTER TABLE public.users
    DROP COLUMN IF EXISTS login_code_exempt,
    DROP COLUMN IF EXISTS login_code_exempt_reason,
    DROP COLUMN IF EXISTS login_code_exempt_by,
    DROP COLUMN IF EXISTS login_code_exempt_at;
