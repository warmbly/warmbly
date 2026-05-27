-- Worker health and capacity model.
--
-- Three additions:
--
--   workers.egress_kind   - what egress profile the worker is configured to
--                            use. Drives the base per-worker capacity ceiling
--                            in worker_capacity_view (cold SMTP is tight,
--                            OAuth API is loose, warmup-only sits in between).
--
--   workers.health_state  - rolled-up health label maintained by the
--                            assignment loop. Authoritative for "can this
--                            worker accept new mailboxes" decisions.
--
--   workers.load_score    - sum of mailbox weights currently assigned, kept
--                            in sync by the assignment service. Cheaper than
--                            counting rows: a Gmail-API mailbox and a cold
--                            SMTP mailbox contribute very different amounts
--                            of load, so account_count is too blunt for
--                            placement decisions.
--
--   worker_health_samples - append-only time-series of per-worker telemetry,
--                            emitted by the worker every 30s. Pruning is the
--                            operator's responsibility - a cron deleting
--                            rows older than 7 days is the recommended
--                            posture.
--
--   worker_capacity_view  - materialized view aggregating the last hour of
--                            samples into the inputs the assignment service
--                            needs. Refreshed on a schedule from the backend
--                            so reads are cheap; the unique index on
--                            worker_id enables REFRESH MATERIALIZED VIEW
--                            CONCURRENTLY (PG14+).

ALTER TABLE workers
    ADD COLUMN egress_kind  TEXT NOT NULL DEFAULT 'cold_smtp'
        CHECK (egress_kind IN ('cold_smtp', 'oauth_api', 'warmup_only')),
    ADD COLUMN health_state TEXT NOT NULL DEFAULT 'healthy'
        CHECK (health_state IN ('healthy', 'watch', 'throttled', 'quarantined', 'blocked')),
    ADD COLUMN load_score   NUMERIC(10, 2) NOT NULL DEFAULT 0;

CREATE INDEX idx_workers_health_capacity
    ON workers (worker_type, free_tier, health_state, load_score)
    WHERE active = true;

CREATE TABLE worker_health_samples (
    id                  BIGSERIAL PRIMARY KEY,
    worker_id           UUID NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_count      INT NOT NULL DEFAULT 0,
    imap_idle_count     INT NOT NULL DEFAULT 0,
    memory_mb           INT NOT NULL DEFAULT 0,
    goroutine_count     INT NOT NULL DEFAULT 0,
    sends_attempted     INT NOT NULL DEFAULT 0,
    sends_succeeded     INT NOT NULL DEFAULT 0,
    bounces_hard        INT NOT NULL DEFAULT 0,
    bounces_soft        INT NOT NULL DEFAULT 0,
    complaints          INT NOT NULL DEFAULT 0,
    auth_errors         INT NOT NULL DEFAULT 0,
    rate_limit_errors   INT NOT NULL DEFAULT 0,
    smtp_latency_p50_ms INT NOT NULL DEFAULT 0,
    smtp_latency_p99_ms INT NOT NULL DEFAULT 0
);

CREATE INDEX idx_worker_health_samples_worker_time
    ON worker_health_samples (worker_id, observed_at DESC);

CREATE MATERIALIZED VIEW worker_capacity_view AS
WITH aggregated AS (
    SELECT worker_id,
           SUM(sends_attempted) AS sends_attempted_1h,
           SUM(sends_succeeded) AS sends_succeeded_1h,
           SUM(bounces_hard)    AS bounces_hard_1h,
           SUM(bounces_soft)    AS bounces_soft_1h,
           SUM(complaints)      AS complaints_1h,
           SUM(auth_errors)     AS auth_errors_1h
      FROM worker_health_samples
     WHERE observed_at > NOW() - INTERVAL '1 hour'
     GROUP BY worker_id
)
SELECT w.id AS worker_id,
       w.worker_type,
       w.free_tier,
       w.egress_kind,
       w.health_state,
       w.load_score,
       -- base ceiling by egress_kind. Cold SMTP is tight (mailbox-bound),
       -- OAuth API is loose (provider does the heavy lifting), warmup-only
       -- sits in between because warmup mailboxes spread across many
       -- participants and don't bottleneck on a single inbox.
       (CASE w.egress_kind
           WHEN 'cold_smtp'   THEN 16
           WHEN 'oauth_api'   THEN 400
           WHEN 'warmup_only' THEN 25
           ELSE 16
        END)::numeric AS base_capacity,
       -- health multiplier from rolling 1h metrics. Each negative signal
       -- removes some capacity; the floor is 0 (we still hand back 1 in
       -- Go so a brand-new worker can be probed).
       GREATEST(0.0, LEAST(1.0,
           1.0
           - LEAST(0.5,
                   COALESCE(a.bounces_hard_1h, 0)::numeric
                   / NULLIF(a.sends_attempted_1h, 0) * 5)
           - LEAST(0.5,
                   COALESCE(a.complaints_1h, 0)::numeric
                   / NULLIF(a.sends_attempted_1h, 0) * 100)
       )) AS health_multiplier,
       -- age ramp: 0.0 to 1.0 over first 72h. New workers start cold and
       -- ramp into their full capacity so reputation has time to build.
       LEAST(1.0,
             EXTRACT(EPOCH FROM (NOW() - w.created_at)) / (72 * 3600)
       ) AS age_multiplier,
       COALESCE(a.sends_attempted_1h, 0) AS sends_attempted_1h,
       COALESCE(a.sends_succeeded_1h, 0) AS sends_succeeded_1h,
       COALESCE(a.bounces_hard_1h, 0)    AS bounces_hard_1h,
       COALESCE(a.bounces_soft_1h, 0)    AS bounces_soft_1h,
       COALESCE(a.complaints_1h, 0)      AS complaints_1h,
       COALESCE(a.auth_errors_1h, 0)     AS auth_errors_1h
  FROM workers w
  LEFT JOIN aggregated a ON a.worker_id = w.id
 WHERE w.active;

CREATE UNIQUE INDEX worker_capacity_view_pk
    ON worker_capacity_view (worker_id);
