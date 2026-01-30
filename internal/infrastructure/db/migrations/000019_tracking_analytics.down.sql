-- Drop tracking deduplication table
DROP TABLE IF EXISTS tracking_events_processed;

-- Drop indexes added for analytics
DROP INDEX IF EXISTS idx_campaign_progress_recent_opens;
DROP INDEX IF EXISTS idx_campaign_progress_recent_clicks;
DROP INDEX IF EXISTS idx_campaign_progress_recent_replies;
DROP INDEX IF EXISTS idx_campaign_progress_recent_bounces;
DROP INDEX IF EXISTS idx_campaign_tasks_tracking;
DROP INDEX IF EXISTS idx_campaign_progress_org_sent;
DROP INDEX IF EXISTS idx_campaign_progress_daily;

-- Note: We don't drop organization_id from campaigns as it may have been added by another migration
