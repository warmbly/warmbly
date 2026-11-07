-- Customer hostnames this instance registered at its edge provider, and the provider's last verdict on each.
CREATE TABLE IF NOT EXISTS edge_hostnames (
    hostname        text PRIMARY KEY CHECK (hostname = lower(hostname) AND hostname <> ''),
    status          text NOT NULL DEFAULT '',
    ownership_name  text NOT NULL DEFAULT '',
    ownership_value text NOT NULL DEFAULT '',
    last_error      text NOT NULL DEFAULT '',
    checked_at      timestamptz,
    unwanted_since  timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
