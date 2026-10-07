ALTER TABLE tasks
    ADD COLUMN send_result_state text CHECK (send_result_state IN ('sent', 'failed', 'unknown')),
    ADD COLUMN send_result_evidence jsonb,
    ADD COLUMN send_result_applied_at timestamptz;

ALTER TABLE email_accounts
    ADD COLUMN send_cooldown_until timestamptz,
    ADD COLUMN send_cooldown_provider text,
    ADD COLUMN send_recovery_hold boolean NOT NULL DEFAULT false,
    ADD COLUMN send_recovery_reason text CHECK (send_recovery_reason IN ('unknown', 'conflict', 'permanent', 'authentication')),
    ADD COLUMN send_recovery_task_id uuid;

CREATE INDEX tasks_send_result_unknown_idx ON tasks (email_account_id)
    WHERE send_result_state = 'unknown';
