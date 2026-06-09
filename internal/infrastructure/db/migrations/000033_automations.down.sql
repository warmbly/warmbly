DROP INDEX IF EXISTS idx_event_subscriptions_automation;
ALTER TABLE integration_event_subscriptions DROP COLUMN IF EXISTS automation_id;
DROP INDEX IF EXISTS idx_automations_org;
DROP TABLE IF EXISTS automations;
