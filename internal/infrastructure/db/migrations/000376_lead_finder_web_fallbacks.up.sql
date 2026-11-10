-- Whether an enrichment run may search the web for a lead's site or published
-- addresses, and which tool of a connected MCP server is a workspace's web search.
-- Runs from before this, and any the previous release still creates, never search.
ALTER TABLE lead_enrichment_runs ADD COLUMN IF NOT EXISTS web_search boolean NOT NULL DEFAULT false;
ALTER TABLE ai_mcp_servers ADD COLUMN IF NOT EXISTS search_tool text NOT NULL DEFAULT '';
