-- 000091_confenge_hardening.up.sql
-- Post go-live hardening: campaign-policy audit binding + imported send readiness.
-- Does not mutate 000090. Absence of readiness fields means autorun forbidden.

-- ── Touchpoint ↔ campaign policy authorization binding ──────────────────────
ALTER TABLE outreach_touchpoints
    ADD COLUMN IF NOT EXISTS campaign_policy_authorization_id uuid,
    ADD COLUMN IF NOT EXISTS authorization_policy_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS authorization_at timestamptz,
    ADD COLUMN IF NOT EXISTS signature_version text NOT NULL DEFAULT '';

ALTER TABLE outreach_touchpoints
    DROP CONSTRAINT IF EXISTS outreach_touchpoints_cpa_fk;

ALTER TABLE outreach_touchpoints
    ADD CONSTRAINT outreach_touchpoints_cpa_fk
        FOREIGN KEY (campaign_policy_authorization_id)
        REFERENCES confenge_campaign_policy_authorizations(id)
        ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS outreach_touchpoints_cpa_id_idx
    ON outreach_touchpoints (campaign_policy_authorization_id)
    WHERE campaign_policy_authorization_id IS NOT NULL;

COMMENT ON COLUMN outreach_touchpoints.campaign_policy_authorization_id IS
'FK to the exact CAMPAIGN_POLICY grant that authorized this touchpoint; null for human approval.';
COMMENT ON COLUMN outreach_touchpoints.authorization_policy_hash IS
'Hash of policy versions + sender + rate + risk at authorization time for audit revalidation.';
COMMENT ON COLUMN outreach_touchpoints.signature_version IS
'Deterministic post-authorization signature decoration version (not material body content).';

-- ── Account-level send fit (imported from extra-cli; never inferred) ─────────
ALTER TABLE outreach_accounts
    ADD COLUMN IF NOT EXISTS target_fit_send_tier text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_fit_reasons jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS email_send_ready boolean NOT NULL DEFAULT false;

ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_target_fit_tier_check;

ALTER TABLE outreach_accounts
    ADD CONSTRAINT outreach_accounts_target_fit_tier_check
        CHECK (target_fit_send_tier IN (
            '', 'A_AUTOMATIC', 'B_EVIDENCE_SUPPORTED', 'RESEARCH_ONLY', 'OUT_OF_SCOPE'
        ));

COMMENT ON COLUMN outreach_accounts.target_fit_send_tier IS
'Imported from extra-cli. Never derive from activation_state. Empty = legacy feed, autorun forbidden.';
COMMENT ON COLUMN outreach_accounts.email_send_ready IS
'Company-level rollup from extra-cli (best contact). Empty/false blocks GREEN autorun.';

-- ── Contact-level readiness (imported; never inferred from email syntax) ─────
ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS email_send_ready boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS mailbox_purpose text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mailbox_purpose_send_blocked boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS ownership_status text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recipient_commercial_suitability text NOT NULL DEFAULT '';

COMMENT ON COLUMN outreach_contact_candidates.email_send_ready IS
'Canonical EMAIL channel readiness from extra-cli. False/absent blocks CAMPAIGN_POLICY autorun.';
COMMENT ON COLUMN outreach_contact_candidates.ownership_status IS
'Imported ownership (COMPANY_OWNED, HUMAN_CONFIRMED, …). Not inferred from address syntax.';
