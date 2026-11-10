-- Agency portal domains registered on the portal edge zone; edge_hostnames keeps the tracking zone's.
CREATE TABLE IF NOT EXISTS portal_edge_hostnames (
    hostname        text PRIMARY KEY CHECK (hostname = lower(hostname) AND hostname <> ''),
    zone            text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT '',
    ownership_name  text NOT NULL DEFAULT '',
    ownership_value text NOT NULL DEFAULT '',
    last_error      text NOT NULL DEFAULT '',
    checked_at      timestamptz,
    unwanted_since  timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
