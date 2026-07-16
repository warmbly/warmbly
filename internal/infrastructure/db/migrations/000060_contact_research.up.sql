-- AI contact research runs. One row per research attempt against a contact.
-- result is a read-then-execute jsonb blob validated at the app boundary by a
-- Go struct (the strict save_research schema), not filtered in SQL. status has a
-- CHECK discriminator. idempotency_key is unique-where-not-null so a retried
-- run (same key) does not double-charge or duplicate a row.

CREATE TABLE IF NOT EXISTS contact_research_runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    contact_id      uuid NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    requested_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'nothing_found')),
    objective       text NOT NULL DEFAULT '',
    result          jsonb NOT NULL DEFAULT '{}'::jsonb,
    error           text NOT NULL DEFAULT '',
    credits_charged integer NOT NULL DEFAULT 0,
    model_used      text NOT NULL DEFAULT '',
    tokens_used     integer NOT NULL DEFAULT 0,
    idempotency_key text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_research_runs_org_contact
    ON contact_research_runs (org_id, contact_id, created_at DESC);

-- Batch drain claims queued rows in FIFO order.
CREATE INDEX IF NOT EXISTS idx_research_runs_queued
    ON contact_research_runs (org_id, created_at ASC)
    WHERE status = 'queued';

CREATE UNIQUE INDEX IF NOT EXISTS uq_research_runs_idempotency
    ON contact_research_runs (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
