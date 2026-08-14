-- 000090_confenge_campaign_policy.up.sql
-- CAMPAIGN_POLICY_AUTHORIZATION: auditable campaign grant for GREEN autorun.
-- Distinct from per-touch HUMAN_TOUCHPOINT_APPROVAL; never forges approved_by.

CREATE TABLE IF NOT EXISTS confenge_campaign_policy_authorizations (
    id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id             uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    campaign_id                 uuid NOT NULL,
    prompt_policy_version       text NOT NULL DEFAULT '',
    validator_version           text NOT NULL DEFAULT '',
    contact_policy_version      text NOT NULL DEFAULT '',
    template_policy_version     text NOT NULL DEFAULT '',
    sender_mailbox              text NOT NULL DEFAULT '',
    channel                     text NOT NULL DEFAULT 'EMAIL',
    allowed_risk_class          text NOT NULL DEFAULT 'GREEN',
    max_rate_per_hour           int NOT NULL DEFAULT 20,
    allow_policy_template_green boolean NOT NULL DEFAULT false,
    effective_at                timestamptz NOT NULL,
    authorized_by               uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    authorized_by_label         text NOT NULL DEFAULT '',
    revoked_at                  timestamptz,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT confenge_cpa_channel_check CHECK (channel IN ('EMAIL', 'WHATSAPP')),
    CONSTRAINT confenge_cpa_risk_check CHECK (allowed_risk_class IN ('GREEN')),
    CONSTRAINT confenge_cpa_rate_check CHECK (max_rate_per_hour >= 1 AND max_rate_per_hour <= 100)
);

CREATE INDEX IF NOT EXISTS confenge_cpa_org_campaign_active_idx
    ON confenge_campaign_policy_authorizations (organization_id, campaign_id, effective_at DESC)
    WHERE revoked_at IS NULL;

-- authorization_mode: '' | HUMAN_TOUCHPOINT_APPROVAL | CAMPAIGN_POLICY
ALTER TABLE outreach_touchpoints
    ADD COLUMN IF NOT EXISTS authorization_mode text NOT NULL DEFAULT '';

ALTER TABLE outreach_touchpoints
    DROP CONSTRAINT IF EXISTS outreach_touchpoints_auth_mode_check;

ALTER TABLE outreach_touchpoints
    ADD CONSTRAINT outreach_touchpoints_auth_mode_check
        CHECK (authorization_mode IN (
            '', 'HUMAN_TOUCHPOINT_APPROVAL', 'CAMPAIGN_POLICY'
        ));

COMMENT ON TABLE confenge_campaign_policy_authorizations IS
'Explicit campaign/policy grant. GREEN messages may autoqueue under EvaluateGreenAutorun without per-touch human approved_by.';
COMMENT ON COLUMN outreach_touchpoints.authorization_mode IS
'HUMAN_TOUCHPOINT_APPROVAL requires approved_by; CAMPAIGN_POLICY uses policy grant and must leave approved_by null.';
