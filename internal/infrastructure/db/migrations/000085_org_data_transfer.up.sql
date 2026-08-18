-- Organization data transfer: portable workspace archives.
--
-- An export walks every org-owned table into a single archive file so a
-- workspace can move between instances (self-host to cloud, cloud to
-- self-host, or one self-host to another). An import reads that archive back.
--
-- Both are long-running and produce an artifact, so they are job rows rather
-- than request-scoped work: the dashboard polls/subscribes for progress and
-- downloads the result when it lands.
--
-- The option columns are typed rather than a jsonb blob because the option set
-- is fixed and small (which data groups, and whether secrets travel). The one
-- jsonb column on each side holds data that is genuinely free-form and only
-- ever read back for display: the source archive's manifest, per-table row
-- counts, and the import's warning list.

CREATE TABLE IF NOT EXISTS org_export_jobs (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    requested_by     uuid REFERENCES users (id) ON DELETE SET NULL,

    status           text NOT NULL DEFAULT 'queued'
                     CHECK (status IN ('queued', 'running', 'completed', 'failed', 'expired')),

    -- Data groups included in this archive. Empty means every group.
    groups           text[] NOT NULL DEFAULT '{}',
    -- Whether mailbox and integration credentials were re-sealed into the
    -- archive under the requester's passphrase. The passphrase itself is
    -- never stored; a wrong one simply fails to unseal at import time.
    include_secrets  boolean NOT NULL DEFAULT false,
    format_version   integer NOT NULL DEFAULT 1,

    progress_percent smallint NOT NULL DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
    progress_stage   text NOT NULL DEFAULT '',

    archive_key      text,
    archive_bytes    bigint,
    archive_sha256   text,
    row_counts       jsonb NOT NULL DEFAULT '{}'::jsonb,

    error_message    text,
    started_at       timestamptz,
    completed_at     timestamptz,
    -- Archives hold a full copy of a workspace, so they are not kept forever.
    expires_at       timestamptz,
    created_at       timestamptz NOT NULL DEFAULT NOW(),
    updated_at       timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_org_export_jobs_org
    ON org_export_jobs (organization_id, created_at DESC);

-- The runner claims work through this index; keeping it partial means the
-- scan stays tiny no matter how much export history accumulates.
CREATE INDEX IF NOT EXISTS idx_org_export_jobs_pending
    ON org_export_jobs (created_at)
    WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_org_export_jobs_expiry
    ON org_export_jobs (expires_at)
    WHERE status = 'completed';

CREATE TABLE IF NOT EXISTS org_import_jobs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    requested_by      uuid REFERENCES users (id) ON DELETE SET NULL,

    status            text NOT NULL DEFAULT 'queued'
                      CHECK (status IN ('queued', 'running', 'completed', 'failed')),

    -- Cleared once the import succeeds: the uploaded archive holds the whole
    -- workspace and is only kept while a retry might still need it.
    archive_key       text,
    archive_bytes     bigint,
    archive_sha256    text,
    -- The manifest as it was written by the source instance. Read back only to
    -- show the operator what they are importing, so it stays a blob.
    source_manifest   jsonb NOT NULL DEFAULT '{}'::jsonb,

    groups            text[] NOT NULL DEFAULT '{}',
    -- What to do when a row in the archive already exists here by primary key.
    conflict_strategy text NOT NULL DEFAULT 'skip'
                      CHECK (conflict_strategy IN ('skip', 'overwrite')),

    progress_percent  smallint NOT NULL DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
    progress_stage    text NOT NULL DEFAULT '',

    row_counts        jsonb NOT NULL DEFAULT '{}'::jsonb,
    warnings          jsonb NOT NULL DEFAULT '[]'::jsonb,

    error_message     text,
    started_at        timestamptz,
    completed_at      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    updated_at        timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_org_import_jobs_org
    ON org_import_jobs (organization_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_org_import_jobs_pending
    ON org_import_jobs (created_at)
    WHERE status IN ('queued', 'running');
