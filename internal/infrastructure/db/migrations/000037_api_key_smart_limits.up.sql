-- Smart per-key rate limits + analytics-friendly indexes.
--
-- rate_limit_per_minute: soft cap enforced in middleware via Redis sliding
-- window. Defaults to 60 r/m (matches the marketing copy on the dashboard);
-- workspace owners can raise/lower it per key. NULL would mean "no limit"
-- but we store an explicit value so the column reads simpler.
--
-- description: human note shown next to the key in the dashboard. Helps
-- when a workspace has many keys for many integrations.
--
-- last_request_ip: most recent caller IP. Useful for "where's this key
-- being used from" without scanning the usage log table.

ALTER TABLE api_keys
    ADD COLUMN rate_limit_per_minute INT NOT NULL DEFAULT 60,
    ADD COLUMN description TEXT,
    ADD COLUMN last_request_ip INET;

-- Analytics: bucketed timeseries grouped by status family. Compound index
-- so the (api_key_id, created_at) → response_status scan is index-only.
CREATE INDEX IF NOT EXISTS idx_api_key_usage_key_time_status
    ON api_key_usage_logs (api_key_id, created_at DESC, response_status);

-- Endpoint breakdown: top endpoints by call count per key.
CREATE INDEX IF NOT EXISTS idx_api_key_usage_endpoint
    ON api_key_usage_logs (api_key_id, endpoint);
