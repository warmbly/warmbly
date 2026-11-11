-- Refuse rather than discard keys an agency entered and the clients it uses them in.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agency_vendor_connections) THEN
        RAISE EXCEPTION 'agencies hold inbox vendor accounts written after 000392; roll forward instead of downgrading past it';
    END IF;
END $$;

DROP INDEX IF EXISTS mailbox_vendor_connections_agency_key;
ALTER TABLE mailbox_vendor_connections DROP COLUMN IF EXISTS agency_connection_id;
DROP TABLE IF EXISTS agency_vendor_connections;
