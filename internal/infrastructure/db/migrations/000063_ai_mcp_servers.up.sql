-- User-connected MCP servers (client direction). An org admin connects an
-- external MCP server; Warmbly discovers its tools and, once the admin enables
-- the server, exposes those tools to the AI assistant (namespaced, always
-- approval-gated). credentials_encrypted is a bearer token sealed with the
-- org DEK (empty for auth_type none). discovered_tools is the last tools/list
-- result (a read-then-execute jsonb blob, validated at the app boundary).
CREATE TABLE IF NOT EXISTS ai_mcp_servers (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name                  text NOT NULL,
    url                   text NOT NULL,
    auth_type             text NOT NULL DEFAULT 'none' CHECK (auth_type IN ('none', 'bearer')),
    credentials_encrypted text NOT NULL DEFAULT '',
    enabled               boolean NOT NULL DEFAULT false,
    discovered_tools      jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_error            text NOT NULL DEFAULT '',
    created_by            uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_mcp_servers_org ON ai_mcp_servers (org_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_mcp_servers_org_name ON ai_mcp_servers (org_id, lower(name));
