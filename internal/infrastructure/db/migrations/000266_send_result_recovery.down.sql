DROP INDEX IF EXISTS tasks_send_result_unknown_idx;
ALTER TABLE tasks DROP COLUMN send_result_state, DROP COLUMN send_result_evidence, DROP COLUMN send_result_applied_at;
ALTER TABLE email_accounts DROP COLUMN send_cooldown_until, DROP COLUMN send_cooldown_provider,
    DROP COLUMN send_recovery_hold, DROP COLUMN send_recovery_reason, DROP COLUMN send_recovery_task_id;
