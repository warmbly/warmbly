-- Remove app-materialized webhook endpoints before dropping the app columns
-- (they were derived from these columns).
DELETE FROM webhook_endpoints WHERE oauth_application_id IS NOT NULL;

ALTER TABLE oauth_applications
    DROP COLUMN IF EXISTS webhook_url,
    DROP COLUMN IF EXISTS webhook_events,
    DROP COLUMN IF EXISTS webhook_secret;
