-- Reverse FK order: drop the child tables (which reference campaigns) before
-- dropping the campaign columns, then the independent contact columns.

DROP INDEX IF EXISTS idx_campaign_senders_campaign;
DROP TABLE IF EXISTS campaign_daily_sends;
DROP TABLE IF EXISTS campaign_senders;

ALTER TABLE contacts DROP COLUMN IF EXISTS esp_resolved_at;
ALTER TABLE contacts DROP COLUMN IF EXISTS esp_provider;

ALTER TABLE campaigns DROP COLUMN IF EXISTS tracking_domain_verified_at;
ALTER TABLE campaigns DROP COLUMN IF EXISTS tracking_domain_verified;
ALTER TABLE campaigns DROP COLUMN IF EXISTS tracking_domain;
ALTER TABLE campaigns DROP COLUMN IF EXISTS prioritize_new_leads;
ALTER TABLE campaigns DROP COLUMN IF EXISTS max_new_leads_per_day;
ALTER TABLE campaigns DROP COLUMN IF EXISTS esp_match_mode;
ALTER TABLE campaigns DROP COLUMN IF EXISTS ramp_level_date;
ALTER TABLE campaigns DROP COLUMN IF EXISTS ramp_level;
ALTER TABLE campaigns DROP COLUMN IF EXISTS ramp_ceiling;
ALTER TABLE campaigns DROP COLUMN IF EXISTS ramp_increment;
ALTER TABLE campaigns DROP COLUMN IF EXISTS ramp_start;
ALTER TABLE campaigns DROP COLUMN IF EXISTS rotation_mode;
ALTER TABLE campaigns DROP COLUMN IF EXISTS sender_strategy;
