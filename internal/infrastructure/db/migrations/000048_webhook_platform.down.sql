DROP TABLE IF EXISTS webhook_event_drops;

DROP INDEX IF EXISTS idx_webhook_deliveries_inflight;
DROP INDEX IF EXISTS idx_webhook_endpoints_oauth_app;

ALTER TABLE webhook_endpoints
    DROP COLUMN IF EXISTS oauth_application_id,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS ownership_confirmed,
    DROP COLUMN IF EXISTS verification_token,
    DROP COLUMN IF EXISTS first_failure_at,
    DROP COLUMN IF EXISTS auto_disabled_at,
    DROP COLUMN IF EXISTS disabled_reason;

ALTER TABLE oauth_applications
    DROP COLUMN IF EXISTS allowed_webhook_domains;
