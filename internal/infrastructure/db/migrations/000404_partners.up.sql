-- Resellers that create and run workspaces through the partner API (/v1/partner).
-- Nothing here has a row until an operator creates a partner, so the API stays
-- off on a self-host that never does. Every table is instance-local: a partner,
-- its keys and its link to a workspace are not part of a workspace archive.

CREATE TABLE public.partners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,39}$'),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    -- What a managed workspace's members read: "Billed through <display_name>".
    display_name text NOT NULL CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 60),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    -- The account that owns every workspace the partner creates and that its API keys act for. It never signs in.
    service_user_id uuid NOT NULL UNIQUE REFERENCES public.users (id) ON DELETE RESTRICT,
    allowed_plans text[] NOT NULL DEFAULT ARRAY['graphkit_managed']::text[],
    -- Hosts a partner-supplied return URL may point at.
    allowed_hosts text[] NOT NULL DEFAULT ARRAY[]::text[],
    -- Domains an operator vouches the partner controls, so its delegation connects may record a grant over them.
    delegation_domains text[] NOT NULL DEFAULT ARRAY[]::text[],
    webhook_url text,
    -- Sealed with CREDENTIALS_ENCRYPTION_KEY; the previous secret keeps signing for a day after a rotation.
    webhook_secret text,
    webhook_secret_previous text,
    webhook_secret_rotated_at timestamptz,
    -- Where the partner hands out per-mailbox access tokens, and the key it issued for that; the key is sealed like the webhook secret.
    callback_base_url text,
    callback_key text,
    -- Wholesale per enabled mailbox-day, in micro-USD. Zero until an operator sets them.
    wholesale_sending_warmup_day_micros bigint NOT NULL DEFAULT 0 CHECK (wholesale_sending_warmup_day_micros >= 0),
    wholesale_warmup_only_day_micros bigint NOT NULL DEFAULT 0 CHECK (wholesale_warmup_only_day_micros >= 0),
    rate_limit_per_second integer NOT NULL DEFAULT 50 CHECK (rate_limit_per_second BETWEEN 20 AND 1000),
    created_by uuid REFERENCES public.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- A partner's API keys. Only the SHA-256 of a key is stored; the mode is part of the key.
CREATE TABLE public.partner_api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE CASCADE,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    key_prefix text NOT NULL,
    key_suffix text NOT NULL,
    key_hash text NOT NULL UNIQUE,
    created_by uuid REFERENCES public.users (id) ON DELETE SET NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_partner_api_keys_partner ON public.partner_api_keys (partner_id, created_at DESC);

-- A workspace a partner manages, under the partner's own id for it. organization_id is
-- cleared when the workspace is deleted, which leaves the row for the sweep that tells the
-- partner and then removes it.
CREATE TABLE public.partner_organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE RESTRICT,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 255),
    organization_id uuid UNIQUE REFERENCES public.organizations (id) ON DELETE SET NULL,
    plan_key text NOT NULL,
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'suspended', 'cancelled')),
    suspended_reason text CHECK (suspended_reason IN ('billing', 'admin', 'reseller')),
    -- Campaign emails one mailbox may send a UTC day, from the partner's latest upsert.
    campaign_sends_per_mailbox_per_day integer NOT NULL DEFAULT 50
        CHECK (campaign_sends_per_mailbox_per_day BETWEEN 1 AND 5000),
    group_external_id text CHECK (group_external_id IS NULL OR char_length(group_external_id) BETWEEN 1 AND 255),
    group_name text,
    -- The group's brand as last sent; free-form and read back whole.
    group_branding jsonb,
    cancelled_at timestamptz,
    deletes_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT partner_organizations_external_key UNIQUE (partner_id, mode, external_id),
    CONSTRAINT partner_organizations_cancel_check CHECK (state <> 'cancelled' OR deletes_at IS NOT NULL)
);
CREATE INDEX idx_partner_organizations_list ON public.partner_organizations (partner_id, mode, created_at DESC, id DESC);
CREATE INDEX idx_partner_organizations_due ON public.partner_organizations (deletes_at) WHERE state = 'cancelled';
CREATE INDEX idx_partner_organizations_gone ON public.partner_organizations (partner_id) WHERE organization_id IS NULL;

-- A person a partner signs in, by the partner's id for them. The account has no password and
-- an address nobody can receive mail at; email is the contact email as last given ('' when none).
CREATE TABLE public.partner_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE RESTRICT,
    -- The key mode that created the person; a key of the other mode never resolves it.
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 255),
    user_id uuid NOT NULL UNIQUE REFERENCES public.users (id) ON DELETE CASCADE,
    email text NOT NULL,
    name text NOT NULL DEFAULT '',
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT partner_identities_external_key UNIQUE (partner_id, mode, external_id)
);

-- The address a partner gave for a person it signs in, shown in place of the account's .invalid
-- sign-in address. It is never a sign-in or a mail destination; NULL when the partner gave none.
ALTER TABLE public.users ADD COLUMN IF NOT EXISTS contact_email text
    CHECK (contact_email IS NULL OR char_length(contact_email) BETWEEN 3 AND 254);

-- One answer per partner, mode and Idempotency-Key for a day. The stored response is sealed
-- with CREDENTIALS_ENCRYPTION_KEY because some carry a secret.
CREATE TABLE public.partner_idempotency_keys (
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE CASCADE,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 255),
    request_hash text NOT NULL,
    status text NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'completed')),
    -- Names the request holding the claim; only that request may complete or release it.
    claim_token uuid NOT NULL DEFAULT gen_random_uuid(),
    status_code integer,
    response_sealed text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (partner_id, mode, key)
);
CREATE INDEX idx_partner_idempotency_keys_created ON public.partner_idempotency_keys (created_at);

-- Events for the partner: delivered to its webhook and readable from GET /events for 30 days.
CREATE TABLE public.partner_events (
    seq bigserial PRIMARY KEY,
    id text NOT NULL UNIQUE,
    partner_id uuid NOT NULL REFERENCES public.partners (id) ON DELETE CASCADE,
    mode text NOT NULL CHECK (mode IN ('live', 'test')),
    type text NOT NULL,
    organization_external_id text,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text,
    abandoned_at timestamptz
);
CREATE INDEX idx_partner_events_feed ON public.partner_events (partner_id, mode, seq);
CREATE INDEX idx_partner_events_due ON public.partner_events (next_attempt_at)
    WHERE delivered_at IS NULL AND abandoned_at IS NULL;
CREATE INDEX idx_partner_events_created ON public.partner_events (created_at);

-- The one plan a partner's workspaces run on: every feature, no caps of its own, no included
-- allowance. Private, never sold through Stripe; what the partner pays is per enabled mailbox.
INSERT INTO plans (
    id, name, max_contacts, daily_emails, ai_generation, account_limit,
    price, discounted_price, duration_id, savings, public,
    dedicated_workers, daily_campaign_limit,
    max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
    monthly_allowance_micro, monthly_credits
) VALUES (
    '00000000-0000-0000-0000-0000000000c1', 'Managed', 0, 0, true, 0,
    0, 0, '00000000-0000-0000-0000-0000000000d1', 0, false,
    0, NULL,
    NULL, NULL, NULL, NULL,
    0, 0
)
ON CONFLICT (id) DO NOTHING;
