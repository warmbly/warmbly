DROP INDEX IF EXISTS mailbox_vendor_connections_agency_key;
ALTER TABLE mailbox_vendor_connections DROP COLUMN IF EXISTS agency_connection_id;
DROP TABLE IF EXISTS agency_vendor_connections;
