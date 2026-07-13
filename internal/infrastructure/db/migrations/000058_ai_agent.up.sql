-- Dashboard AI agent: chat sessions, their message transcript, and per-org
-- tool approval policies.
--
-- agent_sessions is one conversation. context is a read-then-execute jsonb blob
-- (the client's {page, resource} awareness plus any pending tool call awaiting
-- approval); it is validated at the app boundary by a Go struct, not filtered
-- in SQL, so jsonb is the right representation.
--
-- agent_messages is the append-only transcript. content is jsonb holding the
-- provider-agnostic message (role, text, tool_calls, tool results) so a run can
-- be resumed after an approval pause.

CREATE TABLE IF NOT EXISTS agent_sessions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      text NOT NULL DEFAULT '',
    context    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Sessions are per-user; list newest-first for the owning user within an org.
CREATE INDEX IF NOT EXISTS idx_agent_sessions_user
    ON agent_sessions (org_id, user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_messages (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES agent_sessions (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content    jsonb NOT NULL DEFAULT '{}'::jsonb,
    tokens     integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_messages_session
    ON agent_messages (session_id, created_at ASC, id ASC);

-- Per-org tool approval policy. A row means "this org has decided how to handle
-- this tool by default". decision is 'always_allow' (auto-run write tools) — the
-- only persistable policy; send-class tools are never auto-allowed and so never
-- get a row. created_by records who set it.
CREATE TABLE IF NOT EXISTS ai_tool_policies (
    org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    tool_name  text NOT NULL,
    decision   text NOT NULL DEFAULT 'always_allow' CHECK (decision IN ('always_allow')),
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, tool_name)
);
