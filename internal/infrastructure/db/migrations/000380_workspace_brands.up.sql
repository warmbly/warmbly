-- A workspace's own brand on its public pages (connect links, shared reports, forms, the unsubscribe page).
CREATE TABLE IF NOT EXISTS public.workspace_brands (
    organization_id uuid PRIMARY KEY REFERENCES public.organizations(id) ON DELETE CASCADE,
    display_name text NOT NULL DEFAULT '',
    logo_url text NOT NULL DEFAULT '',
    accent_color text NOT NULL DEFAULT '' CHECK (accent_color = '' OR accent_color ~ '^#[0-9a-f]{6}$'),
    website_url text NOT NULL DEFAULT '' CHECK (website_url = '' OR website_url LIKE 'https://%'),
    hide_powered_by boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES public.users(id) ON DELETE SET NULL
);

-- An agency's brand, inherited by every client workspace that sets none of its own.
CREATE TABLE IF NOT EXISTS public.agency_brands (
    agency_id uuid PRIMARY KEY REFERENCES public.agencies(id) ON DELETE CASCADE,
    display_name text NOT NULL DEFAULT '',
    logo_url text NOT NULL DEFAULT '',
    accent_color text NOT NULL DEFAULT '' CHECK (accent_color = '' OR accent_color ~ '^#[0-9a-f]{6}$'),
    website_url text NOT NULL DEFAULT '' CHECK (website_url = '' OR website_url LIKE 'https://%'),
    hide_powered_by boolean NOT NULL DEFAULT false,
    -- One host serving every client workspace's public pages, verified like a forms domain.
    portal_domain text NOT NULL DEFAULT '',
    portal_domain_verified boolean NOT NULL DEFAULT false,
    portal_domain_verified_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES public.users(id) ON DELETE SET NULL
);

-- The TLS ask probe and host ownership look a verified portal host up by name; one agency holds a host.
CREATE UNIQUE INDEX IF NOT EXISTS agency_brands_portal_domain_one_agency
    ON public.agency_brands (portal_domain)
    WHERE portal_domain_verified AND portal_domain <> '';
