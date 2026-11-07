-- Refuse rather than discard the recorded reason a hold was released.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM send_recovery_resolutions WHERE evidence_type = 'provider_access_restored') THEN
        RAISE EXCEPTION 'send_recovery_resolutions holds provider_access_restored releases; roll forward instead of downgrading past 000359';
    END IF;
END $$;
ALTER TABLE send_recovery_resolutions
    DROP CONSTRAINT send_recovery_resolutions_evidence_type_check,
    ADD CONSTRAINT send_recovery_resolutions_evidence_type_check CHECK (evidence_type IN (
        'authentication_repaired', 'operator_provider_confirmation', 'operator_confirmed_sent',
        'operator_confirmed_not_sent', 'authoritative_result_conflict', 'credentials_reverified'));
