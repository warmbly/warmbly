-- Client connect links: a member hands a client a link, and the mailboxes the client connects land in the workspace.
CREATE TABLE client_connect_links (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- Mailboxes connected through the link are attributed to this member; a link without one connects nothing.
    created_by          uuid REFERENCES users (id) ON DELETE SET NULL,
    label               text NOT NULL DEFAULT '',
    -- sha256 of the 32-byte token in the URL; the token itself is never stored.
    token_hash          bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    allowed_providers   text[] NOT NULL,
    max_mailboxes       integer NOT NULL CHECK (max_mailboxes BETWEEN 1 AND 500),
    mailboxes_connected integer NOT NULL DEFAULT 0,
    tag_ids             uuid[] NOT NULL DEFAULT '{}',
    warmup              boolean NOT NULL DEFAULT false,
    expires_at          timestamptz NOT NULL,
    revoked_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT client_connect_links_providers_check
        CHECK (cardinality(allowed_providers) BETWEEN 1 AND 3
               AND allowed_providers <@ ARRAY['gmail', 'outlook', 'smtp_imap']::text[]),
    CONSTRAINT client_connect_links_connected_check
        CHECK (mailboxes_connected BETWEEN 0 AND max_mailboxes)
);

CREATE INDEX client_connect_links_org_idx ON client_connect_links (organization_id, created_at DESC, id DESC);

CREATE TABLE client_connect_link_uses (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    link_id          uuid NOT NULL REFERENCES client_connect_links (id) ON DELETE CASCADE,
    organization_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email_account_id uuid REFERENCES email_accounts (id) ON DELETE SET NULL,
    email            text NOT NULL,
    provider         text NOT NULL,
    remote_ip_hash   text NOT NULL DEFAULT '',
    connected_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX client_connect_link_uses_link_idx ON client_connect_link_uses (link_id, connected_at DESC);
CREATE INDEX client_connect_link_uses_account_idx ON client_connect_link_uses (email_account_id) WHERE email_account_id IS NOT NULL;
