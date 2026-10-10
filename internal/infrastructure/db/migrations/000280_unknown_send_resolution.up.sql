ALTER TABLE send_recovery_resolutions
    DROP CONSTRAINT IF EXISTS send_recovery_resolutions_previous_reason_check,
    DROP CONSTRAINT IF EXISTS send_recovery_resolutions_evidence_type_check,
    ADD CONSTRAINT send_recovery_resolutions_previous_reason_check CHECK(previous_reason IN ('authentication','permanent','conflict','unknown')),
    ADD CONSTRAINT send_recovery_resolutions_evidence_type_check CHECK(evidence_type IN ('authentication_repaired','operator_provider_confirmation','operator_confirmed_sent','operator_confirmed_not_sent')),
    ADD COLUMN IF NOT EXISTS confirmed_result jsonb,
    ADD COLUMN IF NOT EXISTS conflict_detected_at timestamptz;

CREATE UNIQUE INDEX send_recovery_unknown_resolution ON send_recovery_resolutions(recovery_task_id)
    WHERE previous_reason = 'unknown';
CREATE INDEX tasks_unapplied_executor_result ON tasks(created_at)
    WHERE send_result_applied_at IS NULL AND send_executor_result IS NOT NULL;
CREATE INDEX warmup_unapplied_dispatch_result ON warmup_tasks(task_id)
    WHERE dispatch_result IS NOT NULL;
CREATE INDEX tasks_unreserved_unknown_result ON tasks(created_at)
    WHERE send_result_applied_at IS NULL AND status='failed' AND send_result_state='unknown'
    AND send_reserved_at IS NULL AND send_executor_nonce IS NULL AND send_executor_started_at IS NULL;
