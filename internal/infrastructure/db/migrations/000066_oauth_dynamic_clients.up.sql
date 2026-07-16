-- Dynamic Client Registration (RFC 7591) for MCP. AI clients (Claude Code, Cursor,
-- Claude Desktop, ...) self-register as PUBLIC OAuth clients (PKCE, no secret) so a
-- customer connects Warmbly's MCP endpoint with one `claude mcp add` and a browser
-- sign-in instead of pasting an API key. A dynamically-registered client has no
-- owning org/user at registration time: the org + user are chosen by the human at
-- consent and already live on the grant, not the client. So organization_id and
-- created_by become nullable, and two flags describe the client's nature.
ALTER TABLE oauth_applications
    ADD COLUMN is_public boolean NOT NULL DEFAULT false,
    ADD COLUMN dynamically_registered boolean NOT NULL DEFAULT false,
    ALTER COLUMN organization_id DROP NOT NULL,
    ALTER COLUMN created_by DROP NOT NULL;
