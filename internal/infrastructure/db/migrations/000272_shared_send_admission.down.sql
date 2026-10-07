DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM tasks WHERE send_reserved_at IS NOT NULL AND send_released_at IS NULL AND send_result_applied_at IS NULL)
       OR EXISTS (SELECT 1 FROM email_accounts WHERE test_mode IS NOT NULL OR shared_daily_limit IS NOT NULL OR rolling_recipient_limit IS NOT NULL)
       OR EXISTS (SELECT 1 FROM diagnostic_auth_verifications)
       OR EXISTS (SELECT 1 FROM send_recovery_resolutions)
       OR EXISTS (SELECT 1 FROM send_result_effects WHERE delivered_at IS NULL) THEN
        RAISE EXCEPTION 'shared admission or explicit participation state requires retention';
    END IF;
END $$;
DROP TABLE send_result_effects;
DROP TABLE send_recovery_resolutions;
DROP TABLE diagnostic_auth_verifications;
DROP INDEX tasks_send_reservations;
ALTER TABLE tasks DROP COLUMN send_executor_result,DROP COLUMN send_executor_started_at,DROP COLUMN send_executor_worker,
    DROP COLUMN send_executor_nonce,DROP COLUMN send_released_at,DROP COLUMN send_recipients,DROP COLUMN send_business_day,DROP COLUMN send_reserved_at;
ALTER TABLE email_accounts DROP COLUMN rolling_recipient_limit,DROP COLUMN shared_daily_limit,DROP COLUMN test_receive_enabled,DROP COLUMN test_send_enabled,DROP COLUMN test_mode;
