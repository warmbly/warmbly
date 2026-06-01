-- audit_logs: organization-wide activity trail ("who did what, when, from where").
--
-- This is control-plane relational state owned by the backend/consumer. It
-- supersedes the never-wired, user-scoped Cassandra design (the old cdb
-- 000002_audit_logs.up.cql) which had no organization dimension and could not
-- answer org-wide questions.
--
-- Security posture (see internal/app/cipher for why fields are not app-encrypted):
--   * organization_id is NOT NULL and every read is scoped to it server-side
--     (from the caller's session, never a client-supplied value) — this is the
--     primary control against cross-org reads.
--   * ip_address / user_agent / changes / metadata are PII-ish. They are
--     intentionally stored without app-layer envelope encryption: the trail
--     must stay readable org-wide, but the DEK model is strictly per-user, so
--     encrypting an org-wide trail would require every actor's private key.
--     Protect this table with DB-at-rest encryption + access control instead.
--   * Sensitive secret values (API keys, webhook secrets, passwords) must never
--     be written into changes/metadata — the write path records only that a
--     field changed, never its secret value.
--   * Append-only: there is no update/delete API surface. Old rows are removed
--     only by the retention pruning job (default 90 days).
CREATE TABLE IF NOT EXISTS audit_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    action          TEXT NOT NULL,
    entity_type     TEXT NOT NULL,
    entity_id       UUID,
    ip_address      TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    changes         JSONB NOT NULL DEFAULT '{}',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot path: org-wide trail, newest first. Composite covers the keyset cursor
-- (created_at, id) DESC used for pagination.
CREATE INDEX IF NOT EXISTS idx_audit_logs_org_created ON audit_logs (organization_id, created_at DESC, id DESC);

-- "What did this member do" filter.
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor ON audit_logs (organization_id, actor_id, created_at DESC);

-- "History of this entity" filter.
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs (organization_id, entity_type, entity_id, created_at DESC);

-- "Only logins" / action filter.
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (organization_id, action, created_at DESC);

-- Supports the retention pruning job's range delete on created_at.
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs (created_at);
