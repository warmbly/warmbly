-- Migrations from another outreach tool: the job, one target per source workspace, and what each target created.
-- New tables only; nothing an earlier release reads changes.
CREATE TABLE IF NOT EXISTS tool_migrations (
    id uuid PRIMARY KEY,
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    agency_id uuid REFERENCES agencies(id) ON DELETE CASCADE,
    created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    provider text NOT NULL,
    label text NOT NULL DEFAULT '',
    status text NOT NULL,
    -- Sealed with the instance key; NULL once the migration ends or sits idle.
    credentials_sealed text,
    accounts jsonb NOT NULL DEFAULT '[]'::jsonb,
    inventory jsonb NOT NULL DEFAULT '[]'::jsonb,
    options jsonb,
    error text NOT NULL DEFAULT '',
    via_session boolean NOT NULL DEFAULT false,
    -- The scan's holder; a write from any other holder is refused.
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    -- A start in progress: held until then, so client workspaces are only created by one caller.
    starting_until timestamptz,
    -- Client workspaces a start created, by source workspace, so a retried start reuses them.
    created_clients jsonb NOT NULL DEFAULT '{}'::jsonb,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tool_migrations_owner_check CHECK (num_nonnulls(organization_id, agency_id) = 1),
    CONSTRAINT tool_migrations_status_check CHECK (status IN ('scanning', 'ready', 'running', 'completed', 'failed', 'canceled'))
);

CREATE INDEX IF NOT EXISTS idx_tool_migrations_org ON tool_migrations (organization_id, created_at DESC, id DESC) WHERE organization_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tool_migrations_agency ON tool_migrations (agency_id, created_at DESC, id DESC) WHERE agency_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tool_migrations_active ON tool_migrations (status, lease_until) WHERE status IN ('scanning', 'ready', 'running');

CREATE TABLE IF NOT EXISTS tool_migration_targets (
    id uuid PRIMARY KEY,
    migration_id uuid NOT NULL REFERENCES tool_migrations(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id text NOT NULL DEFAULT '',
    key_index integer NOT NULL DEFAULT 0,
    workspace_name text NOT NULL DEFAULT '',
    status text NOT NULL,
    phase text NOT NULL DEFAULT '',
    cursor jsonb,
    counts jsonb NOT NULL DEFAULT '{}'::jsonb,
    followup jsonb NOT NULL DEFAULT '{}'::jsonb,
    error text NOT NULL DEFAULT '',
    mailbox_import_id uuid,
    category_id uuid,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tool_migration_targets_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed', 'canceled')),
    CONSTRAINT tool_migration_targets_workspace_key UNIQUE (migration_id, workspace_id, key_index)
);

CREATE INDEX IF NOT EXISTS idx_tool_migration_targets_org ON tool_migration_targets (organization_id);
CREATE INDEX IF NOT EXISTS idx_tool_migration_targets_claim ON tool_migration_targets (status, lease_until, created_at) WHERE status IN ('pending', 'running');

CREATE TABLE IF NOT EXISTS tool_migration_items (
    target_id uuid NOT NULL REFERENCES tool_migration_targets(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    kind text NOT NULL,
    source_id text NOT NULL,
    warmbly_id uuid NOT NULL,
    -- Reserved before the write it records; a pending row is a write that may not have happened.
    pending boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (target_id, kind, source_id)
);

CREATE INDEX IF NOT EXISTS idx_tool_migration_items_org ON tool_migration_items (organization_id);
