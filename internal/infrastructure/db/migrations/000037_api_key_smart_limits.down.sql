DROP INDEX IF EXISTS idx_api_key_usage_endpoint;
DROP INDEX IF EXISTS idx_api_key_usage_key_time_status;

ALTER TABLE api_keys
    DROP COLUMN IF EXISTS last_request_ip,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS rate_limit_per_minute;
