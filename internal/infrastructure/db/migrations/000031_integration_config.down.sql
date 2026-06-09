-- Reverse 000031. Restoring the strict uniques is best-effort: if rows that
-- violate them were created while the framework was live, the ADD CONSTRAINT
-- will fail and those rows must be de-duplicated first.

ALTER TABLE integration_event_subscriptions
    DROP COLUMN IF EXISTS use_case;

ALTER TABLE integration_event_subscriptions
    ADD CONSTRAINT integration_event_subscriptio_connection_id_event_type_acti_key
        UNIQUE (connection_id, event_type, action);

DROP INDEX IF EXISTS idx_integration_field_mappings_conn;
DROP INDEX IF EXISTS integration_field_mappings_scope_key;

ALTER TABLE integration_field_mappings
    DROP COLUMN IF EXISTS object_name,
    DROP COLUMN IF EXISTS is_default,
    DROP COLUMN IF EXISTS static_value,
    DROP COLUMN IF EXISTS transform,
    DROP COLUMN IF EXISTS subscription_id;

ALTER TABLE integration_field_mappings
    ADD CONSTRAINT integration_field_mappings_connection_id_direction_warmbly__key
        UNIQUE (connection_id, direction, warmbly_field, external_field);

ALTER TABLE integration_connections
    DROP COLUMN IF EXISTS sync_direction,
    DROP COLUMN IF EXISTS config_capabilities;
