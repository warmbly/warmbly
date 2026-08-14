ALTER TABLE outreach_touchpoints DROP CONSTRAINT IF EXISTS outreach_touchpoints_auth_mode_check;
ALTER TABLE outreach_touchpoints DROP COLUMN IF EXISTS authorization_mode;
DROP TABLE IF EXISTS confenge_campaign_policy_authorizations;
