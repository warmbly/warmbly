-- Drop indexes
DROP INDEX IF EXISTS idx_api_key_usage_key;
DROP INDEX IF EXISTS idx_api_key_usage_created;
DROP INDEX IF EXISTS idx_api_keys_prefix;
DROP INDEX IF EXISTS idx_api_keys_user;
DROP INDEX IF EXISTS idx_api_keys_hash;

-- Drop tables
DROP TABLE IF EXISTS api_key_usage_logs;
DROP TABLE IF EXISTS api_keys;
