DROP TRIGGER IF EXISTS integration_inbound_secret_retire_sealed ON integration_connections;
DROP FUNCTION IF EXISTS integration_inbound_secret_retire_sealed();
ALTER TABLE integration_connections
    DROP COLUMN IF EXISTS inbound_secret_digest,
    DROP COLUMN IF EXISTS inbound_secret_encrypted;
