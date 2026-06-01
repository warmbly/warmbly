DROP TABLE IF EXISTS integration_sync_runs;
DROP TABLE IF EXISTS integration_field_mappings;
DROP TABLE IF EXISTS integration_event_subscriptions;
DROP TABLE IF EXISTS integration_oauth_states;

ALTER TABLE integration_connections DROP CONSTRAINT IF EXISTS integration_connections_status_check;
ALTER TABLE integration_connections
    ADD CONSTRAINT integration_connections_status_check
    CHECK (status IN ('pending', 'connected', 'degraded', 'disconnected'));

ALTER TABLE integration_connections
    DROP COLUMN IF EXISTS connected_by_user_id,
    DROP COLUMN IF EXISTS auth_method,
    DROP COLUMN IF EXISTS access_token_encrypted,
    DROP COLUMN IF EXISTS refresh_token_encrypted,
    DROP COLUMN IF EXISTS token_expires_at,
    DROP COLUMN IF EXISTS granted_scopes,
    DROP COLUMN IF EXISTS external_account_id,
    DROP COLUMN IF EXISTS external_account_name,
    DROP COLUMN IF EXISTS health,
    DROP COLUMN IF EXISTS health_detail,
    DROP COLUMN IF EXISTS health_checked_at;
