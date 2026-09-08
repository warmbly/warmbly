-- scheduled_job_runs: one row per background loop on the instance, written by
-- the process that runs it (backend or consumer) and read by the admin panel's
-- Jobs page. Instance-level, not workspace data: it never travels in an
-- organization archive.
CREATE TABLE IF NOT EXISTS scheduled_job_runs (
    name              text PRIMARY KEY,
    service           text NOT NULL DEFAULT '',
    interval_seconds  integer NOT NULL DEFAULT 0,
    last_started_at   timestamptz,
    last_finished_at  timestamptz,
    last_duration_ms  bigint NOT NULL DEFAULT 0,
    last_status       text NOT NULL DEFAULT 'idle'
                      CHECK (last_status IN ('idle', 'running', 'ok', 'error')),
    last_error        text NOT NULL DEFAULT '',
    run_count         bigint NOT NULL DEFAULT 0,
    error_count       bigint NOT NULL DEFAULT 0,
    -- Set by the panel's "run now"; cleared by the owning loop when it picks
    -- the request up on its next poll.
    run_requested_at  timestamptz,
    next_run_at       timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE scheduled_job_runs IS
    'Last run, next run and last error of every background loop, for the admin Jobs page. Instance-level.';
