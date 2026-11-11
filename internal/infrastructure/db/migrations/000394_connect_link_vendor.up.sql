-- A client connect link can also let the client connect an inbox vendor account and pick its mailboxes.
ALTER TABLE client_connect_links DROP CONSTRAINT IF EXISTS client_connect_links_providers_check;
ALTER TABLE client_connect_links ADD CONSTRAINT client_connect_links_providers_check
    CHECK (cardinality(allowed_providers) BETWEEN 1 AND 4
           AND allowed_providers <@ ARRAY['gmail', 'outlook', 'smtp_imap', 'vendor']::text[]);
