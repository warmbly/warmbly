-- Passkeys (WebAuthn credentials).
--
-- Each row is a single FIDO2/WebAuthn credential (a "passkey") bound to a
-- user. Passkeys are an alternative to the password + email-code sign-in:
-- a discoverable credential lets a user authenticate in a single step,
-- so we persist everything go-webauthn needs to verify an assertion and
-- to detect a cloned authenticator (sign_count / clone_warning).
--
-- credential_id and public_key are raw bytes (BYTEA). aaguid identifies
-- the authenticator model and is used only for display (provider name +
-- icon) in the passkey manager, never for a security decision.
CREATE TABLE webauthn_credentials (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id      BYTEA NOT NULL,
    public_key         BYTEA NOT NULL,
    attestation_type   TEXT NOT NULL DEFAULT '',
    attestation_format TEXT NOT NULL DEFAULT '',
    transports         JSONB NOT NULL DEFAULT '[]'::jsonb,
    aaguid             BYTEA NOT NULL DEFAULT ''::bytea,
    sign_count         BIGINT NOT NULL DEFAULT 0,
    clone_warning      BOOLEAN NOT NULL DEFAULT FALSE,
    backup_eligible    BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state       BOOLEAN NOT NULL DEFAULT FALSE,
    user_present       BOOLEAN NOT NULL DEFAULT FALSE,
    user_verified      BOOLEAN NOT NULL DEFAULT FALSE,
    name               TEXT NOT NULL DEFAULT 'Passkey',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at       TIMESTAMPTZ
);

-- A credential ID is globally unique; the discoverable-login lookup and
-- duplicate-enrollment guard both key on it.
CREATE UNIQUE INDEX webauthn_credentials_credential_id_key ON webauthn_credentials (credential_id);

-- List a user's passkeys newest-first for the manager UI.
CREATE INDEX webauthn_credentials_user_id_idx ON webauthn_credentials (user_id, created_at DESC);
