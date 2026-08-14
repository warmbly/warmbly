-- 000091_confenge_hardening.down.sql

ALTER TABLE outreach_touchpoints DROP CONSTRAINT IF EXISTS outreach_touchpoints_cpa_fk;
DROP INDEX IF EXISTS outreach_touchpoints_cpa_id_idx;
ALTER TABLE outreach_touchpoints
    DROP COLUMN IF EXISTS campaign_policy_authorization_id,
    DROP COLUMN IF EXISTS authorization_policy_hash,
    DROP COLUMN IF EXISTS authorization_at,
    DROP COLUMN IF EXISTS signature_version;

ALTER TABLE outreach_accounts DROP CONSTRAINT IF EXISTS outreach_accounts_target_fit_tier_check;
ALTER TABLE outreach_accounts
    DROP COLUMN IF EXISTS target_fit_send_tier,
    DROP COLUMN IF EXISTS target_fit_reasons,
    DROP COLUMN IF EXISTS email_send_ready;

ALTER TABLE outreach_contact_candidates
    DROP COLUMN IF EXISTS email_send_ready,
    DROP COLUMN IF EXISTS mailbox_purpose,
    DROP COLUMN IF EXISTS mailbox_purpose_send_blocked,
    DROP COLUMN IF EXISTS ownership_status,
    DROP COLUMN IF EXISTS recipient_commercial_suitability;
