-- Federated identities, keyed on (provider, issuer, subject).
--
-- External sign-in previously resolved the account by email alone. That is
-- defensible for Apple and Google, which control their own email namespace,
-- but it becomes an account-takeover primitive the moment an operator points
-- a generic OIDC issuer at a provider where anyone can self-register an
-- arbitrary address: sign up there as someone@company.com and you are them
-- here. Coder shipped that exact bug (CVE-2026-55076).
--
-- The subject claim is the only stable, provider-controlled identifier, so it
-- is what the account is bound to. Email is a fallback used once, to link a
-- pre-existing local account, and only when the provider says the address is
-- verified and no other identity from that issuer already claims that user.

CREATE TABLE IF NOT EXISTS user_identities (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- provider is the mechanism ('oidc', 'google', 'apple'), issuer is the
    -- verified iss claim, subject is the sub claim.
    provider     text NOT NULL,
    issuer       text NOT NULL,
    subject      text NOT NULL,

    email        text,
    created_at   timestamptz NOT NULL DEFAULT NOW(),
    last_login_at timestamptz,

    CONSTRAINT user_identities_provider_check CHECK (provider IN ('oidc', 'google', 'apple'))
);

-- One account per (issuer, subject). The unique index is the actual security
-- control: it makes it impossible for two users to claim the same identity.
CREATE UNIQUE INDEX IF NOT EXISTS user_identities_issuer_subject_idx
    ON user_identities (issuer, subject);

CREATE INDEX IF NOT EXISTS user_identities_user_id_idx
    ON user_identities (user_id);
