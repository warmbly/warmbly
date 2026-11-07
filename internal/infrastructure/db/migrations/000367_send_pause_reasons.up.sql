-- Why a mailbox's timed send pause exists, how often it repeated, what the provider said, and how often a held mailbox was re-probed.
ALTER TABLE email_accounts
    ADD COLUMN send_cooldown_reason text,
    ADD COLUMN send_cooldown_strikes integer NOT NULL DEFAULT 0,
    ADD COLUMN send_pause_detail text,
    ADD COLUMN send_recovery_probes integer NOT NULL DEFAULT 0,
    ADD COLUMN send_recovery_probe_at timestamptz;

ALTER TABLE email_accounts
    ADD CONSTRAINT email_accounts_send_cooldown_reason_check
        CHECK (send_cooldown_reason IN ('retry', 'rate_limited', 'daily_limit', 'sender_blocked')) NOT VALID,
    DROP CONSTRAINT email_accounts_send_recovery_reason_check,
    ADD CONSTRAINT email_accounts_send_recovery_reason_check
        CHECK (send_recovery_reason IN ('unknown', 'conflict', 'permanent', 'authentication', 'domain_auth')) NOT VALID;
ALTER TABLE email_accounts VALIDATE CONSTRAINT email_accounts_send_cooldown_reason_check;
ALTER TABLE email_accounts VALIDATE CONSTRAINT email_accounts_send_recovery_reason_check;

ALTER TABLE send_recovery_resolutions
    DROP CONSTRAINT send_recovery_resolutions_previous_reason_check,
    ADD CONSTRAINT send_recovery_resolutions_previous_reason_check
        CHECK (previous_reason IN ('authentication', 'permanent', 'conflict', 'unknown', 'domain_auth')),
    DROP CONSTRAINT send_recovery_resolutions_evidence_type_check,
    ADD CONSTRAINT send_recovery_resolutions_evidence_type_check CHECK (evidence_type IN (
        'authentication_repaired', 'operator_provider_confirmation', 'operator_confirmed_sent',
        'operator_confirmed_not_sent', 'authoritative_result_conflict', 'credentials_reverified',
        'provider_access_restored', 'hold_probe', 'operator_resumed', 'domain_auth_passing'));

-- The probe sweep looks only at held mailboxes.
CREATE INDEX email_accounts_send_recovery_probe_idx ON email_accounts (send_recovery_probe_at)
    WHERE send_recovery_hold AND send_recovery_reason IN ('permanent', 'domain_auth');
