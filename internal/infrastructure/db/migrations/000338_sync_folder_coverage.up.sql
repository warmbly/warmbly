-- Additive counters preserve reads and writes from v0.6.72 during rollout and rollback.
ALTER TABLE email_sync_state
    ADD COLUMN folders_skipped_cap integer NOT NULL DEFAULT 0 CHECK (folders_skipped_cap >= 0),
    ADD COLUMN folders_skipped_conflict integer NOT NULL DEFAULT 0 CHECK (folders_skipped_conflict >= 0);
