-- Additive counters preserve reads and writes from v0.6.72 during rollout and rollback.
ALTER TABLE email_sync_state
    ADD COLUMN folders_skipped_cap integer NOT NULL DEFAULT 0,
    ADD COLUMN folders_skipped_conflict integer NOT NULL DEFAULT 0,
    ADD COLUMN folder_coverage_observed_at timestamptz,
    ADD CONSTRAINT email_sync_state_folders_skipped_cap_check CHECK (folders_skipped_cap >= 0) NOT VALID,
    ADD CONSTRAINT email_sync_state_folders_skipped_conflict_check CHECK (folders_skipped_conflict >= 0) NOT VALID;
