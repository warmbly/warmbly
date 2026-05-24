-- Free-form admin-applied tags on workers.
--
-- The fixed attributes (worker_type, free_tier, risk_pool) cover the
-- dimensions the assignment logic actually cares about. Tags exist for
-- everything else admins want to organize by: region (eu-west-1, fra,
-- us-east), provider (hetzner, ovh, vultr), role (warmup-only,
-- burst-capacity), customer cohort, anything.
--
-- Schema:
--   - composite PK (worker_id, tag) so the same tag can't be applied twice
--   - tag is lowercase, dashed; constraint just rejects empty/too-long
--   - ON DELETE CASCADE on worker so removing a worker drops its tags
--
-- "Smart" auto-derived labels (tier:free, pool:risky, state:error) are
-- NOT stored here — they're computed from the worker row on read. Keeps
-- the storage model simple and the auto-labels always in sync.

CREATE TABLE worker_tags (
    worker_id UUID NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    tag VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (worker_id, tag),
    CONSTRAINT valid_tag CHECK (length(tag) > 0 AND tag ~ '^[a-z0-9][a-z0-9._:/-]*$')
);

CREATE INDEX idx_worker_tags_tag ON worker_tags(tag);
