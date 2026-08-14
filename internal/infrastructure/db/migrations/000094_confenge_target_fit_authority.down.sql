DROP TABLE IF EXISTS outreach_target_fit_reconciliation_events;
DROP INDEX IF EXISTS outreach_accounts_org_target_fit_operational_idx;

ALTER TABLE outreach_accounts DROP CONSTRAINT IF EXISTS outreach_accounts_queue_state_check;
UPDATE outreach_accounts
SET queue_state = CASE
    WHEN do_not_contact THEN 'DO_NOT_CONTACT'
    WHEN blocked THEN 'BLOCKED'
    WHEN email_send_ready THEN 'READY_TO_GENERATE'
    ELSE 'NEEDS_CONTACT'
END
WHERE queue_state = 'TARGET_FIT_SUPPRESSED';
ALTER TABLE outreach_accounts ADD CONSTRAINT outreach_accounts_queue_state_check
    CHECK (queue_state IN (
        'NEEDS_CONTACT','READY_TO_GENERATE','NEEDS_REVIEW','APPROVED','ENROLLED',
        'SENT','REPLIED','MEETING','PROPOSAL','WON','LOST','BLOCKED','BOUNCED',
        'DO_NOT_CONTACT','SKIPPED'
    ));

ALTER TABLE outreach_accounts
    DROP COLUMN IF EXISTS target_fit_reconciled_at,
    DROP COLUMN IF EXISTS target_fit_suppression_reason,
    DROP COLUMN IF EXISTS target_fit_eligible,
    DROP COLUMN IF EXISTS target_fit_freshness_reason,
    DROP COLUMN IF EXISTS target_fit_evidence_ids,
    DROP COLUMN IF EXISTS target_fit_fresh,
    DROP COLUMN IF EXISTS target_fit_observed_at,
    DROP COLUMN IF EXISTS target_fit_source_watermark,
    DROP COLUMN IF EXISTS target_fit_computed_at,
    DROP COLUMN IF EXISTS target_fit_version,
    DROP COLUMN IF EXISTS target_fit_confidence,
    DROP COLUMN IF EXISTS target_fit_class;
