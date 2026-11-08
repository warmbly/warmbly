-- Agency API keys: one credential that reaches every client workspace of an
-- agency, and the agency endpoints. Expand only; the previous release ignores it.

CREATE TABLE IF NOT EXISTS agency_api_keys (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agency_id             uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    -- The staff member the key acts for; it stops when they are no longer an owner or admin.
    created_by            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                  text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 255),
    key_prefix            text NOT NULL,
    key_suffix            text NOT NULL,
    key_hash              text NOT NULL UNIQUE,
    permissions           bigint NOT NULL CHECK (permissions <> 0),
    allowed_ips           text[] NOT NULL DEFAULT '{}',
    rate_limit_per_minute integer NOT NULL DEFAULT 60 CHECK (rate_limit_per_minute BETWEEN 1 AND 10000),
    status                text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    last_used_at          timestamptz,
    last_request_ip       inet,
    expires_at            timestamptz,
    revoked_at            timestamptz,
    revoked_reason        text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agency_api_keys_agency ON agency_api_keys (agency_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_agency_api_keys_creator ON agency_api_keys (created_by) WHERE status = 'active';

-- Idempotency records for agency-key calls to agency endpoints, which have no workspace.
-- Old releases always write organization_id and read by it, so they never see these rows.
ALTER TABLE api_idempotency_keys
    ADD COLUMN IF NOT EXISTS agency_id uuid REFERENCES agencies (id) ON DELETE CASCADE;
ALTER TABLE api_idempotency_keys
    ALTER COLUMN organization_id DROP NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'api_idempotency_keys_one_scope') THEN
        ALTER TABLE api_idempotency_keys
            ADD CONSTRAINT api_idempotency_keys_one_scope
            CHECK ((organization_id IS NULL) <> (agency_id IS NULL)) NOT VALID;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS api_idempotency_keys_agency_key
    ON api_idempotency_keys (agency_id, key) WHERE agency_id IS NOT NULL;
