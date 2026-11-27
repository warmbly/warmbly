-- Alone in its file for CONCURRENTLY. Finds a workspace's contacts by their handle on a channel.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contact_social_identities_lookup
    ON public.contact_social_identities (organization_id, channel, normalized);
