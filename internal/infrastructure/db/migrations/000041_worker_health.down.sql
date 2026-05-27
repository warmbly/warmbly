DROP MATERIALIZED VIEW IF EXISTS worker_capacity_view;

DROP INDEX IF EXISTS idx_worker_health_samples_worker_time;
DROP TABLE IF EXISTS worker_health_samples;

DROP INDEX IF EXISTS idx_workers_health_capacity;

ALTER TABLE workers
    DROP COLUMN IF EXISTS load_score,
    DROP COLUMN IF EXISTS health_state,
    DROP COLUMN IF EXISTS egress_kind;
