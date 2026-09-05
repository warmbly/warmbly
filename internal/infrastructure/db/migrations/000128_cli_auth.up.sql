-- CLI sign-in: the `warmbly` CLI has no credential of its own, so it opens a
-- device-code handshake, a signed-in member approves it in the browser, and the
-- approval mints an ordinary API key. The key is the credential; this table
-- only carries the handshake and is empty within minutes.
--
-- Same shape as pool_link_codes, one row per `warmbly auth login`.

CREATE TABLE IF NOT EXISTS cli_auth_codes (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code_hash text NOT NULL UNIQUE,
    user_code        text NOT NULL UNIQUE,
    -- What the CLI asked for, shown on the approval screen.
    client_name      text NOT NULL DEFAULT '',
    hostname         text NOT NULL DEFAULT '',
    cli_version      text NOT NULL DEFAULT '',
    scopes           bigint NOT NULL DEFAULT 0,
    status           text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'claimed', 'denied')),
    organization_id  uuid REFERENCES organizations (id) ON DELETE CASCADE,
    approved_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    api_key_id       uuid REFERENCES api_keys (id) ON DELETE SET NULL,
    -- The minted secret, held only between approval and the CLI's next poll.
    api_key_secret   text,
    expires_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cli_auth_codes_expires ON cli_auth_codes (expires_at);
