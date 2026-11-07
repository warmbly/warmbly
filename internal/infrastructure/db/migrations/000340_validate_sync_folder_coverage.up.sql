-- A separate transaction validates existing rows while mailbox reads and writes continue.
ALTER TABLE email_sync_state
    VALIDATE CONSTRAINT email_sync_state_folders_skipped_cap_check,
    VALIDATE CONSTRAINT email_sync_state_folders_skipped_conflict_check;
