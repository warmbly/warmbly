DROP TABLE IF EXISTS preflight_reports;
DROP TABLE IF EXISTS reply_intents;
DROP TABLE IF EXISTS task_dead_letters;
DROP TABLE IF EXISTS task_execution_keys;
DROP TABLE IF EXISTS suppressed_recipients;
DROP TABLE IF EXISTS deliverability_events;
DROP TABLE IF EXISTS campaign_ab_assignments;
DROP TABLE IF EXISTS campaign_ab_variants;
DROP TABLE IF EXISTS campaign_advanced_settings;
DROP TABLE IF EXISTS outreach_settings;

-- task_status enum values are intentionally not removed in down migration.
