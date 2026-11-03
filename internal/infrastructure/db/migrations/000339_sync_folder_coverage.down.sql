ALTER TABLE email_sync_state
    DROP COLUMN folders_skipped_cap,
    DROP COLUMN folders_skipped_conflict,
    DROP COLUMN folder_coverage_observed_at;
