DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM email_accounts WHERE send_recovery_hold OR send_cooldown_until > NOW())
       OR EXISTS (SELECT 1 FROM tasks WHERE send_result_state = 'unknown') THEN
        RAISE EXCEPTION 'cannot remove unresolved send recovery holds or active cooldowns';
    END IF;
END $$;

DROP INDEX IF EXISTS tasks_send_result_unknown_idx;
ALTER TABLE tasks DROP COLUMN send_result_state, DROP COLUMN send_result_evidence, DROP COLUMN send_result_applied_at;
ALTER TABLE email_accounts DROP COLUMN send_cooldown_until, DROP COLUMN send_cooldown_provider,
    DROP COLUMN send_recovery_hold, DROP COLUMN send_recovery_reason, DROP COLUMN send_recovery_task_id;
