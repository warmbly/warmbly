-- Make extra-cli current target-fit the commercial authority for CONFENGE.

ALTER TABLE outreach_accounts
    ADD COLUMN IF NOT EXISTS target_fit_class text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_fit_confidence double precision,
    ADD COLUMN IF NOT EXISTS target_fit_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_fit_computed_at timestamptz,
    ADD COLUMN IF NOT EXISTS target_fit_source_watermark text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_fit_observed_at timestamptz,
    ADD COLUMN IF NOT EXISTS target_fit_fresh boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS target_fit_evidence_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS target_fit_freshness_reason text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_fit_eligible boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS target_fit_suppression_reason text NOT NULL DEFAULT 'TARGET_FIT_MISSING',
    ADD COLUMN IF NOT EXISTS target_fit_reconciled_at timestamptz;

ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_target_fit_confidence_check;
ALTER TABLE outreach_accounts
    ADD CONSTRAINT outreach_accounts_target_fit_confidence_check
        CHECK (target_fit_confidence IS NULL OR (target_fit_confidence >= 0 AND target_fit_confidence <= 1));

ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_queue_state_check;
ALTER TABLE outreach_accounts
    ADD CONSTRAINT outreach_accounts_queue_state_check
        CHECK (queue_state IN (
            'NEEDS_CONTACT','READY_TO_GENERATE','NEEDS_REVIEW','APPROVED','ENROLLED',
            'SENT','REPLIED','MEETING','PROPOSAL','WON','LOST','BLOCKED','BOUNCED',
            'DO_NOT_CONTACT','SKIPPED','TARGET_FIT_SUPPRESSED'
        ));

CREATE INDEX IF NOT EXISTS outreach_accounts_org_target_fit_operational_idx
    ON outreach_accounts (organization_id, target_fit_eligible, email_send_ready, queue_state);

COMMENT ON COLUMN outreach_accounts.target_fit_class IS
'Authoritative current ICP class imported from extra-cli. Warmbly never derives it.';
COMMENT ON COLUMN outreach_accounts.target_fit_observed_at IS
'Comparable target-fit decision watermark used to reject temporal regression.';
COMMENT ON COLUMN outreach_accounts.target_fit_suppression_reason IS
'Reversible commercial suppression reason. It is distinct from sticky human DNC/block.';

CREATE TABLE IF NOT EXISTS outreach_target_fit_reconciliation_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE CASCADE,
    target_fit_class text NOT NULL DEFAULT '',
    target_fit_version text NOT NULL DEFAULT '',
    target_fit_source_watermark text NOT NULL DEFAULT '',
    eligible boolean NOT NULL DEFAULT false,
    reason text NOT NULL,
    cancelled_touchpoints int NOT NULL DEFAULT 0,
    blocked_drafts int NOT NULL DEFAULT 0,
    detached_enrollments int NOT NULL DEFAULT 0,
    cancelled_dispatch_items int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outreach_target_fit_reconciliation_org_created_idx
    ON outreach_target_fit_reconciliation_events (organization_id, created_at DESC);

-- Existing rows predate the authoritative class/freshness contract. Fail closed
-- while preserving sent/replied/history rows and every account record.
UPDATE outreach_accounts
SET target_fit_eligible = false,
    target_fit_suppression_reason = 'TARGET_FIT_MISSING',
    target_fit_reconciled_at = now(),
    queue_state = CASE
        WHEN queue_state IN ('NEEDS_CONTACT','READY_TO_GENERATE','NEEDS_REVIEW','APPROVED','ENROLLED')
            THEN 'TARGET_FIT_SUPPRESSED'
        ELSE queue_state
    END
WHERE target_fit_class = '';

-- Record what the one-time backfill is about to revoke. The account, draft,
-- touchpoint and this event remain as the commercial audit trail.
INSERT INTO outreach_target_fit_reconciliation_events (
    organization_id, account_id, target_fit_class, target_fit_version,
    target_fit_source_watermark, eligible, reason, cancelled_touchpoints,
    blocked_drafts, detached_enrollments, cancelled_dispatch_items
)
SELECT
    a.organization_id,
    a.id,
    a.target_fit_class,
    a.target_fit_version,
    a.target_fit_source_watermark,
    false,
    a.target_fit_suppression_reason,
    (
        SELECT count(*)::int FROM outreach_touchpoints tp
        WHERE tp.account_id = a.id
          AND tp.state IN ('PLANNED','DUE','DRAFTED','NEEDS_REVIEW','APPROVED','QUEUED')
    ),
    (
        SELECT count(*)::int FROM outreach_drafts d
        WHERE d.account_id = a.id
          AND d.status IN ('NOT_GENERATED','GENERATING','NEEDS_REVIEW','APPROVED','ENROLLED')
    ),
    (
        SELECT count(*)::int
        FROM campaign_leads cl
        JOIN outreach_drafts d
          ON d.campaign_id = cl.campaign_id AND d.enrollment_contact_id = cl.contact_id
        WHERE d.account_id = a.id
          AND d.status IN ('NOT_GENERATED','GENERATING','NEEDS_REVIEW','APPROVED','ENROLLED')
    ),
    (
        SELECT count(*)::int
        FROM confenge_dispatch_queue q
        JOIN outreach_drafts d ON d.id = q.draft_id
        WHERE d.account_id = a.id AND q.status IN ('queued','reserved')
    )
FROM outreach_accounts a
WHERE a.target_fit_eligible = false;

UPDATE outreach_touchpoints tp
SET state = 'CANCELLED',
    stop_reason = 'TARGET_FIT_MISSING',
    approved_by = NULL,
    approved_at = NULL,
    approved_content_hash = '',
    authorization_mode = '',
    campaign_policy_authorization_id = NULL,
    authorization_policy_hash = '',
    authorization_at = NULL,
    updated_at = now()
FROM outreach_accounts a
WHERE a.id = tp.account_id
  AND a.target_fit_eligible = false
  AND tp.state IN ('PLANNED','DUE','DRAFTED','NEEDS_REVIEW','APPROVED','QUEUED');

UPDATE outreach_drafts d
SET status = 'BLOCKED', updated_at = now()
FROM outreach_accounts a
WHERE a.id = d.account_id
  AND a.target_fit_eligible = false
  AND d.status IN ('NOT_GENERATED','GENERATING','NEEDS_REVIEW','APPROVED','ENROLLED');

UPDATE confenge_dispatch_queue q
SET status = 'cancelled', cancel_reason = 'TARGET_FIT_MISSING', updated_at = now()
FROM outreach_drafts d
JOIN outreach_accounts a ON a.id = d.account_id
WHERE q.draft_id = d.id
  AND a.target_fit_eligible = false
  AND q.status IN ('queued','reserved');

DELETE FROM campaign_leads cl
USING outreach_drafts d, outreach_accounts a
WHERE cl.campaign_id = d.campaign_id
  AND cl.contact_id = d.enrollment_contact_id
  AND d.account_id = a.id
  AND a.target_fit_eligible = false
  AND d.status = 'BLOCKED';
