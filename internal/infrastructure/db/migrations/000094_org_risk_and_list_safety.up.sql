ALTER TYPE public.task_status ADD VALUE IF NOT EXISTS 'skipped_content_guardrail';
ALTER TYPE public.task_status ADD VALUE IF NOT EXISTS 'skipped_org_suspended';

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS risk_state text NOT NULL DEFAULT 'trusted',
    ADD COLUMN IF NOT EXISTS risk_score integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS risk_reason text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS risk_signals jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS risk_evaluated_at timestamptz;

ALTER TABLE organizations
    DROP CONSTRAINT IF EXISTS organizations_risk_state_check,
    ADD CONSTRAINT organizations_risk_state_check
        CHECK (risk_state IN ('trusted', 'watch', 'restricted', 'suspended')),
    DROP CONSTRAINT IF EXISTS organizations_risk_score_check,
    ADD CONSTRAINT organizations_risk_score_check
        CHECK (risk_score BETWEEN 0 AND 100);

CREATE INDEX IF NOT EXISTS idx_organizations_risk_state
    ON organizations (risk_state, risk_score DESC)
    WHERE risk_state <> 'trusted';

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS signup_ip inet,
    ADD COLUMN IF NOT EXISTS signup_user_agent text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS signup_email_risk integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS signup_asn bigint;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_signup_email_risk_check,
    ADD CONSTRAINT users_signup_email_risk_check
        CHECK (signup_email_risk BETWEEN 0 AND 100);

CREATE INDEX IF NOT EXISTS idx_users_signup_ip
    ON users (signup_ip)
    WHERE signup_ip IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_signup_asn
    ON users (signup_asn)
    WHERE signup_asn IS NOT NULL;

ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS payment_fingerprint text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_subscriptions_payment_fingerprint
    ON subscriptions (payment_fingerprint)
    WHERE payment_fingerprint <> '';

ALTER TABLE email_accounts
    ADD COLUMN IF NOT EXISTS cold_ramp_started_at timestamptz;

ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS verification_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS verification_checked_at timestamptz;

ALTER TABLE campaigns
    DROP CONSTRAINT IF EXISTS campaigns_verification_status_check,
    ADD CONSTRAINT campaigns_verification_status_check
        CHECK (verification_status IN ('unknown', 'pending', 'passed', 'warning', 'blocked'));

CREATE TABLE IF NOT EXISTS contact_import_assessments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename text NOT NULL DEFAULT '',
    total_rows integer NOT NULL,
    invalid_rows integer NOT NULL DEFAULT 0,
    disposable_rows integer NOT NULL DEFAULT 0,
    role_rows integer NOT NULL DEFAULT 0,
    risky_tld_rows integer NOT NULL DEFAULT 0,
    blocked boolean NOT NULL DEFAULT false,
    summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    CHECK (total_rows >= 0),
    CHECK (invalid_rows >= 0),
    CHECK (disposable_rows >= 0),
    CHECK (role_rows >= 0),
    CHECK (risky_tld_rows >= 0)
);

CREATE INDEX IF NOT EXISTS idx_contact_import_assessments_org_created
    ON contact_import_assessments (organization_id, created_at DESC);
