-- The mailbox half of the partner API: mailboxes a partner attaches or that are connected in a
-- workspace it manages, what each may do (enablement) and since when, the partner's hosted
-- connect links, the brand of a partner's group, and the monthly statement a partner is billed
-- from. Nothing here has a row until an operator creates a partner, and all of it is
-- instance-local: none of it travels in a workspace archive.

-- One row per mailbox of a partner-managed workspace. id is the partner API's mailbox id. A row
-- outlives its mailbox (email_account_id is cleared), so enabled days stay reportable.
CREATE TABLE public.partner_mailboxes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE RESTRICT,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    org_external_id text NOT NULL CHECK (char_length(org_external_id) BETWEEN 1 AND 255),
    organization_id uuid REFERENCES public.organizations (id) ON DELETE SET NULL,
    email_account_id uuid UNIQUE REFERENCES public.email_accounts (id) ON DELETE SET NULL,
    -- The partner's own id for the mailbox; NULL for one connected inside Warmbly.
    partner_mailbox_id text CHECK (partner_mailbox_id IS NULL OR char_length(partner_mailbox_id) BETWEEN 1 AND 255),
    source text NOT NULL CHECK (source IN ('graphkit_infrastructure', 'partner', 'manual')),
    email text NOT NULL,
    provider text NOT NULL CHECK (provider IN ('google', 'microsoft', 'smtp')),
    first_name text NOT NULL DEFAULT '',
    last_name text NOT NULL DEFAULT '',
    enablement text NOT NULL DEFAULT 'off' CHECK (enablement IN ('off', 'warmup_only', 'sending_warmup')),
    connection_method text CHECK (connection_method IN ('token', 'delegation', 'password', 'oauth')),
    -- A token or delegation attach connects in the background: pending until a worker claims it.
    connect_state text NOT NULL DEFAULT 'connected' CHECK (connect_state IN ('pending', 'connecting', 'connected', 'failed')),
    connect_error text,
    connect_attempts integer NOT NULL DEFAULT 0,
    next_connect_at timestamptz,
    detached_at timestamptz,
    -- The mailbox was removed inside Warmbly; the partner was told.
    removed_at timestamptz,
    -- What the partner was last told about the mailbox, so a change is announced once.
    published_state text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX partner_mailboxes_partner_key ON public.partner_mailboxes (partner_id, mode, org_external_id, partner_mailbox_id)
    WHERE partner_mailbox_id IS NOT NULL;
CREATE INDEX idx_partner_mailboxes_org ON public.partner_mailboxes (organization_id, created_at, id);
CREATE INDEX idx_partner_mailboxes_list ON public.partner_mailboxes (partner_id, mode, org_external_id, created_at, id);
CREATE INDEX idx_partner_mailboxes_connect ON public.partner_mailboxes (next_connect_at)
    WHERE connect_state IN ('pending', 'connecting');

-- When each mailbox had which enablement; an open row (ended_at NULL) is in force. Off has no row.
CREATE TABLE public.partner_mailbox_enablements (
    id bigserial PRIMARY KEY,
    mailbox_id uuid NOT NULL REFERENCES public.partner_mailboxes (id) ON DELETE CASCADE,
    enablement text NOT NULL CHECK (enablement IN ('warmup_only', 'sending_warmup')),
    started_at timestamptz NOT NULL,
    ended_at timestamptz,
    CONSTRAINT partner_mailbox_enablements_span CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE UNIQUE INDEX partner_mailbox_enablements_open ON public.partner_mailbox_enablements (mailbox_id) WHERE ended_at IS NULL;
CREATE INDEX idx_partner_mailbox_enablements_span ON public.partner_mailbox_enablements (mailbox_id, started_at);

-- A connect link a partner asked for: which mailbox it connects, as what, and where the person returns.
CREATE TABLE public.partner_connect_links (
    link_id uuid PRIMARY KEY REFERENCES public.client_connect_links (id) ON DELETE CASCADE,
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE CASCADE,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    org_external_id text NOT NULL,
    partner_mailbox_id text NOT NULL CHECK (char_length(partner_mailbox_id) BETWEEN 1 AND 255),
    provider text NOT NULL CHECK (provider IN ('google', 'microsoft')),
    enablement text NOT NULL CHECK (enablement IN ('off', 'warmup_only', 'sending_warmup')),
    login_hint text NOT NULL DEFAULT '',
    return_url text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- A partner's group (a company, an agency or a reseller) and the brand every workspace sent with it shows.
CREATE TABLE public.partner_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE RESTRICT,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 255),
    name text NOT NULL DEFAULT '',
    product_name text NOT NULL DEFAULT '',
    logo_url text NOT NULL DEFAULT '',
    accent_color text NOT NULL DEFAULT '',
    website_url text NOT NULL DEFAULT '',
    hide_powered_by boolean NOT NULL DEFAULT false,
    portal_domain text NOT NULL DEFAULT '',
    portal_status text NOT NULL DEFAULT 'none' CHECK (portal_status IN ('none', 'pending', 'verified', 'failed')),
    portal_checked_at timestamptz,
    portal_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT partner_groups_external_key UNIQUE (partner_id, mode, external_id)
);
CREATE INDEX idx_partner_groups_portal ON public.partner_groups (portal_checked_at NULLS FIRST)
    WHERE portal_domain <> '' AND portal_status <> 'verified';

-- A closed month of a partner's enabled mailbox-days at the rates in force when it closed. Final once written.
CREATE TABLE public.partner_statements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE RESTRICT,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    month date NOT NULL CHECK (extract(day FROM month) = 1),
    sending_warmup_days bigint NOT NULL DEFAULT 0,
    warmup_only_days bigint NOT NULL DEFAULT 0,
    sending_warmup_day_micros bigint NOT NULL DEFAULT 0,
    warmup_only_day_micros bigint NOT NULL DEFAULT 0,
    amount_micros bigint NOT NULL DEFAULT 0,
    closed_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT partner_statements_month_key UNIQUE (partner_id, mode, month)
);

CREATE TABLE public.partner_statement_lines (
    statement_id uuid NOT NULL REFERENCES public.partner_statements (id) ON DELETE CASCADE,
    org_external_id text NOT NULL,
    sending_warmup_days integer NOT NULL DEFAULT 0,
    warmup_only_days integer NOT NULL DEFAULT 0,
    amount_micros bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (statement_id, org_external_id)
);
