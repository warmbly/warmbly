-- PostgreSQL cannot remove enum values. Normalize rows before rolling back.
UPDATE tasks
SET status = 'completed', completed_at = COALESCE(completed_at, NOW())
WHERE status IN ('skipped_content_guardrail', 'skipped_org_suspended');

DROP TABLE IF EXISTS contact_import_assessments;

ALTER TABLE campaigns
    DROP CONSTRAINT IF EXISTS campaigns_verification_status_check,
    DROP COLUMN IF EXISTS verification_checked_at,
    DROP COLUMN IF EXISTS verification_summary,
    DROP COLUMN IF EXISTS verification_status;

ALTER TABLE email_accounts
    DROP COLUMN IF EXISTS cold_ramp_started_at;

DROP INDEX IF EXISTS idx_subscriptions_payment_fingerprint;
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS payment_fingerprint;

DROP INDEX IF EXISTS idx_users_signup_asn;
DROP INDEX IF EXISTS idx_users_signup_ip;
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_signup_email_risk_check,
    DROP COLUMN IF EXISTS signup_asn,
    DROP COLUMN IF EXISTS signup_email_risk,
    DROP COLUMN IF EXISTS signup_user_agent,
    DROP COLUMN IF EXISTS signup_ip;

DROP INDEX IF EXISTS idx_organizations_risk_state;
ALTER TABLE organizations
    DROP CONSTRAINT IF EXISTS organizations_risk_score_check,
    DROP CONSTRAINT IF EXISTS organizations_risk_state_check,
    DROP COLUMN IF EXISTS risk_evaluated_at,
    DROP COLUMN IF EXISTS risk_signals,
    DROP COLUMN IF EXISTS risk_reason,
    DROP COLUMN IF EXISTS risk_score,
    DROP COLUMN IF EXISTS risk_state;
