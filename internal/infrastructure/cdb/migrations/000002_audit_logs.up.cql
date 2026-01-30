USE maildata;

// AUDIT LOGS - Tracks user actions with 90-day TTL
// Primary table partitioned by user_id and date for efficient user-based queries
CREATE TABLE audit_logs (
    user_id              uuid,
    action_date          date,
    timestamp            timestamp,
    id                   uuid,

    action               text,           -- create, update, delete, login, logout, etc.
    entity_type          text,           -- campaign, contact, email_account, api_key, etc.
    entity_id            uuid,

    ip_address           text,
    user_agent           text,

    changes              map<text, text>,  -- field -> "old_value -> new_value"
    metadata             map<text, text>,  -- additional context

    PRIMARY KEY ((user_id, action_date), timestamp, id)
) WITH CLUSTERING ORDER BY (timestamp DESC, id ASC)
AND default_time_to_live = 7776000;  -- 90 days (3 months)

// Secondary table for entity-based queries (admin use)
CREATE TABLE audit_logs_by_entity (
    entity_type          text,
    entity_id            uuid,
    timestamp            timestamp,
    id                   uuid,
    user_id              uuid,
    action               text,

    PRIMARY KEY ((entity_type, entity_id), timestamp, id)
) WITH CLUSTERING ORDER BY (timestamp DESC, id ASC)
AND default_time_to_live = 7776000;  -- 90 days (3 months)
