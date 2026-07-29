DROP INDEX IF EXISTS uq_research_runs_idempotency;

CREATE UNIQUE INDEX IF NOT EXISTS uq_research_runs_idempotency
    ON contact_research_runs (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
