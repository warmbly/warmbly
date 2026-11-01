-- Websites a workspace proved with its _warmbly TXT record, so its redirects may send visitors there.
CREATE TABLE website_domains (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain text NOT NULL CHECK (domain = lower(domain) AND domain <> ''),
    verified_at timestamptz NOT NULL DEFAULT now(),
    last_checked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, domain)
);

CREATE INDEX website_domains_checked_idx ON website_domains (last_checked_at);
