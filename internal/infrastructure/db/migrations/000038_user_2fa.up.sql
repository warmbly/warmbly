-- TOTP 2FA state, isolated from the users table (so 2FA + key rotation never
-- touch user rows). The secret is sealed with a SERVER-WIDE key (the per-user
-- DEK is unreachable at login time, when verification happens).
CREATE TABLE public.user_totp_settings (
    user_id            uuid PRIMARY KEY REFERENCES public.users (id) ON DELETE CASCADE,
    totp_secret_sealed text NOT NULL,                 -- AES-256-GCM(server key), base64
    totp_enabled       boolean NOT NULL DEFAULT false,
    confirmed_at       timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Single-use recovery codes, argon2-hashed at rest (shown to the user once).
CREATE TABLE public.user_totp_recovery_codes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    code_hash  text NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_totp_recovery_user_unused ON public.user_totp_recovery_codes (user_id) WHERE used_at IS NULL;
