-- An inbox vendor account (Zapmail, InboxKit, ...) an agency holds once and uses in any of its client workspaces.
-- The API key is sealed with the instance key: an agency has no data key of its own.
CREATE TABLE IF NOT EXISTS agency_vendor_connections (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agency_id   uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    vendor      text NOT NULL,
    label       text NOT NULL DEFAULT '',
    credentials text NOT NULL,
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agency_vendor_connections_agency ON agency_vendor_connections (agency_id);

-- A workspace's connection made from an agency's account. It ends with the account; its mailboxes stay.
ALTER TABLE mailbox_vendor_connections
    ADD COLUMN IF NOT EXISTS agency_connection_id uuid REFERENCES agency_vendor_connections (id) ON DELETE CASCADE;

-- One per workspace and account, so using an account in a client twice finds the first.
CREATE UNIQUE INDEX IF NOT EXISTS mailbox_vendor_connections_agency_key
    ON mailbox_vendor_connections (organization_id, agency_connection_id) WHERE agency_connection_id IS NOT NULL;
