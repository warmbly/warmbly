-- Configurable integration framework. Three changes turn the integration tables
-- from "blind, one-shape" into "fully configurable per connection":
--
--  1. Connections carry an onboarding/capability snapshot + a sync direction.
--  2. The previously-dead integration_field_mappings table is wired so each
--     automation (or the connection default) can map Warmbly fields to provider
--     fields, with transforms and static values.
--  3. Event subscriptions gain a use_case discriminator and lose the strict
--     one-row-per-(connection,event,action) unique so a connection can hold
--     several differently-filtered automations for the same event.

-- 1) Connection-level onboarding/capability snapshot (selected objects, enabled
--    use-cases, picker selections). Distinct from the sealed config_encrypted
--    secrets blob. sync_direction defaults to push (the proven path); pull/both
--    are modelled now and implemented later.
ALTER TABLE integration_connections
    ADD COLUMN config_capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN sync_direction text NOT NULL DEFAULT 'push'
        CONSTRAINT integration_connections_direction_check
        CHECK (sync_direction = ANY (ARRAY['push'::text, 'pull'::text, 'both'::text]));

-- 2) Wire the field-mappings table: scope a map to one automation (or leave NULL
--    for the connection default), support transforms + static/constant values,
--    and tag the target object so a connection can map contacts AND deals.
ALTER TABLE integration_field_mappings
    ADD COLUMN subscription_id uuid REFERENCES integration_event_subscriptions (id) ON DELETE CASCADE,
    ADD COLUMN transform text NOT NULL DEFAULT 'none',
    ADD COLUMN static_value text NOT NULL DEFAULT '',
    ADD COLUMN is_default boolean NOT NULL DEFAULT false,
    ADD COLUMN object_name text NOT NULL DEFAULT 'contact';

-- The old unique (connection_id, direction, warmbly_field, external_field) is too
-- strict now that maps are scoped per automation + object. Replace it with a
-- scope-aware unique that treats a NULL subscription_id (connection default) as a
-- single bucket via COALESCE to the nil UUID.
ALTER TABLE integration_field_mappings
    DROP CONSTRAINT IF EXISTS integration_field_mappings_connection_id_direction_warmbly__key;

-- Uniqueness is per DESTINATION field within a scope: each provider field has
-- one mapping, but a Warmbly source field may legitimately feed several
-- destination fields. A NULL subscription_id (connection default) is one bucket.
CREATE UNIQUE INDEX integration_field_mappings_scope_key
    ON integration_field_mappings (
        connection_id,
        object_name,
        direction,
        COALESCE(subscription_id, '00000000-0000-0000-0000-000000000000'::uuid),
        external_field
    );

CREATE INDEX idx_integration_field_mappings_conn
    ON integration_field_mappings (connection_id, object_name);

-- 3) Per-automation use-case discriminator (drives projection + which handler).
ALTER TABLE integration_event_subscriptions
    ADD COLUMN use_case text NOT NULL DEFAULT 'custom';

-- Allow several differently-filtered automations per (connection, event, action)
-- so a user gets full flexibility (e.g. two reply automations with different
-- intent filters routing to different channels). Row identity is the id PK.
ALTER TABLE integration_event_subscriptions
    DROP CONSTRAINT IF EXISTS integration_event_subscriptio_connection_id_event_type_acti_key;
