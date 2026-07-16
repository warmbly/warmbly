-- Scope the research idempotency key per org. A client-supplied Idempotency-Key
-- must never match another tenant's run, so the uniqueness (and the replay
-- lookup in the repo) are keyed by (org_id, idempotency_key), not the key alone.
DROP INDEX IF EXISTS uq_research_runs_idempotency;

CREATE UNIQUE INDEX IF NOT EXISTS uq_research_runs_idempotency
    ON contact_research_runs (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
