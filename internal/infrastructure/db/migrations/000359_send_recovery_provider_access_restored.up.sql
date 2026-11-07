-- A mailbox-wide API refusal whose mailbox has since synced in full is released, recorded as its own evidence.
ALTER TABLE send_recovery_resolutions
    DROP CONSTRAINT send_recovery_resolutions_evidence_type_check,
    ADD CONSTRAINT send_recovery_resolutions_evidence_type_check CHECK (evidence_type IN (
        'authentication_repaired', 'operator_provider_confirmation', 'operator_confirmed_sent',
        'operator_confirmed_not_sent', 'authoritative_result_conflict', 'credentials_reverified',
        'provider_access_restored'));
