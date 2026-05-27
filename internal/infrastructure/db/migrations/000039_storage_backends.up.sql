-- storage_backends is the registry of pluggable infrastructure backends
-- (KMS, encrypted-key store, blob store, event bus, cache) that the admin UI
-- displays under Settings.
--
-- For each kind, exactly one row may have is_active = true; that's what the
-- backend processes use. Inactive rows are kept so operators can compare
-- previously-active configs and roll back.
--
-- Important: not every kind is mutable via the admin UI. KMS and encrypted_keys
-- in particular are chicken-and-egg with the cipher service — changing them at
-- runtime would orphan existing ciphertext. The UI surfaces those as read-only
-- (env-var driven). The DB row exists so the UI has something to display.
--
-- config is jsonb. Sensitive values inside (API keys, credentials) MUST be
-- stored encrypted via the cipher service before insert — never plaintext.
-- Application code is responsible for that, not the schema.

CREATE TABLE IF NOT EXISTS storage_backends (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         TEXT NOT NULL CHECK (kind IN ('kms','encrypted_keys','blob','eventbus','cache')),
    provider     TEXT NOT NULL,
    name         TEXT NOT NULL,
    config       JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active    BOOLEAN NOT NULL DEFAULT FALSE,
    is_readonly  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Exactly one active row per kind. Partial unique index makes attempting a
-- second active row a database-level error rather than relying on app logic.
CREATE UNIQUE INDEX IF NOT EXISTS storage_backends_active_per_kind
    ON storage_backends (kind)
    WHERE is_active;
