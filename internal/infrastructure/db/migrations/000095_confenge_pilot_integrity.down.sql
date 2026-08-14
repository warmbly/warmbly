DROP TABLE IF EXISTS outreach_pilot_memberships;
DROP TABLE IF EXISTS outreach_pilot_slots;
DROP TABLE IF EXISTS outreach_pilot_operations;

ALTER TABLE outreach_feed_sync_state
    DROP COLUMN IF EXISTS source_generated_at;

ALTER TABLE outreach_import_runs
    DROP COLUMN IF EXISTS source_generated_at;
