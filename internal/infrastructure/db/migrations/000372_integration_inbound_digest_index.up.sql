-- Alone in its file for CONCURRENTLY. Serves the inbound delivery lookup by secret digest.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_integration_connections_inbound_digest
    ON public.integration_connections (provider, inbound_secret_digest)
    WHERE inbound_secret_digest IS NOT NULL;
