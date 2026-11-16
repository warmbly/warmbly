-- Refuse rather than change links a client may already hold.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM client_connect_links WHERE 'vendor' = ANY (allowed_providers)) THEN
        RAISE EXCEPTION 'connect links take a vendor account, written after 000394; roll forward instead of downgrading past it';
    END IF;
END $$;

ALTER TABLE client_connect_links DROP CONSTRAINT IF EXISTS client_connect_links_providers_check;
ALTER TABLE client_connect_links ADD CONSTRAINT client_connect_links_providers_check
    CHECK (cardinality(allowed_providers) BETWEEN 1 AND 3
           AND allowed_providers <@ ARRAY['gmail', 'outlook', 'smtp_imap']::text[]);
