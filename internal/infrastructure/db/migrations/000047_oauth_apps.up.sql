-- OAuth2 authorization server. Third-party apps register here; org members grant
-- them scoped access via the authorization-code flow (every app holds a client
-- secret, with optional PKCE on top); the issued bearer tokens authenticate API
-- calls with the SAME permission bitmask as API keys, reusing every route gate.

-- A registered third-party application (the OAuth client).
CREATE TABLE oauth_applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    logo_url text NOT NULL DEFAULT '',
    website_url text NOT NULL DEFAULT '',
    client_id text NOT NULL UNIQUE,
    -- SHA-256 of the client secret (one-way, like api_keys.key_hash). Every app
    -- is issued a secret and authenticates the token exchange with it.
    client_secret_hash text NOT NULL DEFAULT '',
    -- Exact-match redirect URIs (no fuzzy matching).
    redirect_uris text[] NOT NULL DEFAULT '{}',
    -- Bitmask of the API permissions this app is allowed to request.
    scopes bigint NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_oauth_applications_org ON oauth_applications (organization_id, created_at DESC);

-- A short-lived authorization code minted on user consent, consumed once at the
-- token endpoint. Holds the PKCE challenge + the exact scopes/redirect granted.
CREATE TABLE oauth_authorization_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash text NOT NULL UNIQUE,
    application_id uuid NOT NULL REFERENCES oauth_applications (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    redirect_uri text NOT NULL,
    scopes bigint NOT NULL DEFAULT 0,
    -- PKCE is an optional extra layer; empty when the app did not send a challenge.
    code_challenge text NOT NULL DEFAULT '',
    code_challenge_method text NOT NULL DEFAULT '',
    used_at timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- An issued access+refresh token pair. Tokens are stored hashed (lookup by hash,
-- like API keys); the refresh token rotates on every use.
CREATE TABLE oauth_access_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES oauth_applications (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    scopes bigint NOT NULL DEFAULT 0,
    access_token_hash text NOT NULL UNIQUE,
    refresh_token_hash text UNIQUE,
    access_expires_at timestamptz NOT NULL,
    refresh_expires_at timestamptz,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_oauth_grants_app_org ON oauth_access_grants (application_id, organization_id);
CREATE INDEX idx_oauth_grants_org_user ON oauth_access_grants (organization_id, user_id) WHERE revoked_at IS NULL;
