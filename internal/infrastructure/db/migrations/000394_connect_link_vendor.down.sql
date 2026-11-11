-- A link that only took a vendor account has no way left to connect, so it is turned off.
UPDATE client_connect_links SET revoked_at = COALESCE(revoked_at, now()), allowed_providers = ARRAY['smtp_imap']::text[]
WHERE allowed_providers = ARRAY['vendor']::text[];
UPDATE client_connect_links SET allowed_providers = array_remove(allowed_providers, 'vendor') WHERE 'vendor' = ANY (allowed_providers);
ALTER TABLE client_connect_links DROP CONSTRAINT IF EXISTS client_connect_links_providers_check;
ALTER TABLE client_connect_links ADD CONSTRAINT client_connect_links_providers_check
    CHECK (cardinality(allowed_providers) BETWEEN 1 AND 3
           AND allowed_providers <@ ARRAY['gmail', 'outlook', 'smtp_imap']::text[]);
