-- Per-automation run history: one row per fired automation (the graph walk),
-- with per-node outcomes in a jsonb column. Best-effort observability so a
-- silent failure (down integration, deleted connection, missed condition) is
-- visible in the builder instead of only in server logs.
CREATE TABLE automation_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id uuid NOT NULL REFERENCES automations (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    trigger_event text NOT NULL,
    status text NOT NULL DEFAULT 'running'
        CHECK (status = ANY (ARRAY['running', 'success', 'error'])),
    node_results jsonb NOT NULL DEFAULT '[]'::jsonb,
    error_detail text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

CREATE INDEX idx_automation_runs_automation ON automation_runs (automation_id, started_at DESC);
CREATE INDEX idx_automation_runs_org ON automation_runs (organization_id, started_at DESC);
