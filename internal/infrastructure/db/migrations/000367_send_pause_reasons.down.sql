-- Refuse rather than discard the recorded reason a hold was released.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM send_recovery_resolutions
               WHERE previous_reason = 'domain_auth' OR evidence_type IN ('hold_probe', 'operator_resumed', 'domain_auth_passing')) THEN
        RAISE EXCEPTION 'send_recovery_resolutions holds releases recorded after 000367; roll forward instead of downgrading past it';
    END IF;
END $$;

DROP INDEX IF EXISTS email_accounts_send_recovery_probe_idx;

-- A domain-authentication hold stays a hold under the older reason set.
UPDATE email_accounts SET send_recovery_reason = 'permanent' WHERE send_recovery_reason = 'domain_auth';

ALTER TABLE send_recovery_resolutions
    DROP CONSTRAINT send_recovery_resolutions_previous_reason_check,
    ADD CONSTRAINT send_recovery_resolutions_previous_reason_check
        CHECK (previous_reason IN ('authentication', 'permanent', 'conflict', 'unknown')),
    DROP CONSTRAINT send_recovery_resolutions_evidence_type_check,
    ADD CONSTRAINT send_recovery_resolutions_evidence_type_check CHECK (evidence_type IN (
        'authentication_repaired', 'operator_provider_confirmation', 'operator_confirmed_sent',
        'operator_confirmed_not_sent', 'authoritative_result_conflict', 'credentials_reverified',
        'provider_access_restored'));

ALTER TABLE email_accounts
    DROP CONSTRAINT email_accounts_send_recovery_reason_check,
    ADD CONSTRAINT email_accounts_send_recovery_reason_check
        CHECK (send_recovery_reason IN ('unknown', 'conflict', 'permanent', 'authentication')),
    DROP CONSTRAINT email_accounts_send_cooldown_reason_check,
    DROP COLUMN send_recovery_probe_at,
    DROP COLUMN send_recovery_probes,
    DROP COLUMN send_pause_detail,
    DROP COLUMN send_cooldown_strikes,
    DROP COLUMN send_cooldown_reason;
