-- Linked mailboxes warm only on Warmbly Cloud with its own copy of the credential.
DELETE FROM pool_link_self_execution_grants;

-- A workspace's own Google or Microsoft OAuth client, used for every sign-in it starts.
CREATE TABLE IF NOT EXISTS organization_oauth_apps (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    provider        email_provider NOT NULL CHECK (provider IN ('gmail', 'outlook')),
    client_id       text NOT NULL CHECK (btrim(client_id) <> ''),
    -- Sealed with the organization's data key; empty after an import that carried no secrets.
    client_secret   text NOT NULL DEFAULT '',
    app_name        text NOT NULL CHECK (btrim(app_name) <> ''),
    -- Empty means this deployment's own callback.
    redirect_url    text NOT NULL DEFAULT '',
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, provider)
);

-- The client that issued a mailbox's grant; NULL is this deployment's own client.
ALTER TABLE email_accounts_oauth
    ADD COLUMN IF NOT EXISTS oauth_app_id uuid REFERENCES organization_oauth_apps (id);

-- A single-use sign-in link a mailbox owner opens to connect a Google or Microsoft mailbox.
CREATE TABLE IF NOT EXISTS mailbox_connect_links (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_by       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider         email_provider NOT NULL CHECK (provider IN ('gmail', 'outlook')),
    -- SHA-256 of the secret in the link; the secret itself is never stored.
    token_hash       bytea NOT NULL UNIQUE,
    expected_email   text NOT NULL DEFAULT '',
    warmup           jsonb NOT NULL DEFAULT '{}'::jsonb,
    return_url       text NOT NULL DEFAULT '',
    status           text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'connecting', 'connected', 'failed', 'revoked')),
    error_code       text NOT NULL DEFAULT '',
    email_account_id uuid REFERENCES email_accounts (id) ON DELETE SET NULL,
    expires_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz
);

CREATE INDEX IF NOT EXISTS idx_mailbox_connect_links_org
    ON mailbox_connect_links (organization_id, created_at DESC, id DESC);
