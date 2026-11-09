-- The inbound URL secret is sealed with the organization DEK and found by its SHA-256 digest.
ALTER TABLE integration_connections
    ADD COLUMN IF NOT EXISTS inbound_secret_encrypted text,
    ADD COLUMN IF NOT EXISTS inbound_secret_digest text;

-- A writer that changes only inbound_secret (the previous release) retires the sealed copy, so a rotated URL never keeps resolving.
-- An import's '' sentinel stays, since it is what flags the connection for reconnection.
CREATE FUNCTION integration_inbound_secret_retire_sealed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.inbound_secret IS DISTINCT FROM OLD.inbound_secret
       AND NEW.inbound_secret_digest IS NOT DISTINCT FROM OLD.inbound_secret_digest THEN
        IF NEW.inbound_secret_encrypted IS DISTINCT FROM '' THEN
            NEW.inbound_secret_encrypted := NULL;
        END IF;
        NEW.inbound_secret_digest := NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER integration_inbound_secret_retire_sealed
BEFORE UPDATE OF inbound_secret ON integration_connections
FOR EACH ROW EXECUTE FUNCTION integration_inbound_secret_retire_sealed();

COMMENT ON COLUMN public.integration_connections.inbound_secret_encrypted IS
    'Inbound URL secret sealed with the organization DEK.';
COMMENT ON COLUMN public.integration_connections.inbound_secret_digest IS
    'Hex SHA-256 of the inbound URL secret, the lookup key for deliveries.';
