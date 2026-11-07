-- Lead finder: local businesses found on a map through a pluggable place
-- provider, enriched from their own websites, and turned into contacts.

ALTER TABLE public.contacts DROP CONSTRAINT contacts_source_check;
ALTER TABLE public.contacts
    ADD CONSTRAINT contacts_source_check
    CHECK (source IN ('unknown', 'manual', 'campaign', 'import', 'sheet_sync', 'api', 'ai_assistant', 'form', 'automation', 'crm_sync', 'apollo', 'clay', 'lead_finder')) NOT VALID;

-- A search someone ran: what, where and through which provider.
CREATE TABLE lead_searches (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id    uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    name               text NOT NULL DEFAULT '',
    provider           text NOT NULL CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper')),
    query              text NOT NULL DEFAULT '',
    category           text NOT NULL DEFAULT '',
    location_label     text NOT NULL DEFAULT '',
    area               jsonb NOT NULL,
    saved              boolean NOT NULL DEFAULT false,
    result_count       integer NOT NULL DEFAULT 0,
    pages_fetched      integer NOT NULL DEFAULT 0,
    next_cursor        text NOT NULL DEFAULT '',
    last_run_at        timestamptz NOT NULL DEFAULT NOW(),
    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_lead_searches_org ON lead_searches (organization_id, last_run_at DESC);

-- One business, once per workspace and provider record.
CREATE TABLE leads (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    provider          text NOT NULL CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper')),
    external_id       text NOT NULL CHECK (external_id <> ''),
    name              text NOT NULL DEFAULT '',
    category          text NOT NULL DEFAULT '',
    categories        text[] NOT NULL DEFAULT '{}',
    address           text NOT NULL DEFAULT '',
    street            text NOT NULL DEFAULT '',
    city              text NOT NULL DEFAULT '',
    region            text NOT NULL DEFAULT '',
    postcode          text NOT NULL DEFAULT '',
    country           text NOT NULL DEFAULT '',
    latitude          double precision NOT NULL,
    longitude         double precision NOT NULL,
    phone             text NOT NULL DEFAULT '',
    website           text NOT NULL DEFAULT '',
    domain            text NOT NULL DEFAULT '',
    email             text NOT NULL DEFAULT '',
    emails            jsonb NOT NULL DEFAULT '[]'::jsonb,
    socials           jsonb NOT NULL DEFAULT '{}'::jsonb,
    rating            double precision,
    review_count      integer,
    price_level       smallint,
    hours             jsonb NOT NULL DEFAULT '[]'::jsonb,
    business_status   text NOT NULL DEFAULT '',
    maps_url          text NOT NULL DEFAULT '',
    description       text NOT NULL DEFAULT '',
    chain             text NOT NULL DEFAULT '',
    site              jsonb NOT NULL DEFAULT '{}'::jsonb,
    score             smallint NOT NULL DEFAULT 0,
    status            text NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'saved', 'contacted', 'dismissed')),
    enrichment_status text NOT NULL DEFAULT 'none' CHECK (enrichment_status IN ('none', 'queued', 'done', 'no_website', 'failed')),
    enriched_at       timestamptz,
    contact_id        uuid REFERENCES contacts (id) ON DELETE SET NULL,
    tags              text[] NOT NULL DEFAULT '{}',
    notes             text NOT NULL DEFAULT '',
    first_seen_at     timestamptz NOT NULL DEFAULT NOW(),
    last_seen_at      timestamptz NOT NULL DEFAULT NOW(),
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    updated_at        timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, provider, external_id)
);

CREATE INDEX idx_leads_org_seen ON leads (organization_id, last_seen_at DESC, id DESC);
CREATE INDEX idx_leads_org_score ON leads (organization_id, score DESC, id DESC);
CREATE INDEX idx_leads_org_geo ON leads (organization_id, latitude, longitude);
CREATE INDEX idx_leads_contact ON leads (contact_id) WHERE contact_id IS NOT NULL;

-- Which searches returned a lead, so a saved search reopens with its results.
CREATE TABLE lead_search_hits (
    search_id uuid NOT NULL REFERENCES lead_searches (id) ON DELETE CASCADE,
    lead_id   uuid NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    seen_at   timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (search_id, lead_id)
);

CREATE INDEX idx_lead_search_hits_lead ON lead_search_hits (lead_id);

-- One request to read a set of leads' websites for emails and socials. Runs in
-- the background; the row is its progress and outcome.
CREATE TABLE lead_enrichment_runs (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    requested_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    lead_ids             uuid[] NOT NULL,
    verify               boolean NOT NULL DEFAULT true,
    people               boolean NOT NULL DEFAULT true,
    status               text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'error')),
    total                integer NOT NULL DEFAULT 0,
    processed            integer NOT NULL DEFAULT 0,
    emails_found         integer NOT NULL DEFAULT 0,
    people_found         integer NOT NULL DEFAULT 0,
    no_website           integer NOT NULL DEFAULT 0,
    failed               integer NOT NULL DEFAULT 0,
    credits_consumed     integer NOT NULL DEFAULT 0,
    last_error           text NOT NULL DEFAULT '',
    started_at           timestamptz,
    finished_at          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT NOW(),
    updated_at           timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_lead_enrichment_runs_org ON lead_enrichment_runs (organization_id, created_at DESC);
CREATE INDEX idx_lead_enrichment_runs_open ON lead_enrichment_runs (status, updated_at) WHERE status IN ('queued', 'running');

-- People who work at a lead's business: read off its website or typed in by
-- hand, with the address found for them or worked out from the domain's pattern.
CREATE TABLE lead_people (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    lead_id          uuid NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (name <> ''),
    name_key         text NOT NULL CHECK (name_key <> ''),
    first_name       text NOT NULL DEFAULT '',
    last_name        text NOT NULL DEFAULT '',
    title            text NOT NULL DEFAULT '',
    seniority        text NOT NULL DEFAULT 'staff' CHECK (seniority IN ('owner', 'executive', 'manager', 'staff')),
    email            text NOT NULL DEFAULT '',
    email_source     text NOT NULL DEFAULT '' CHECK (email_source IN ('', 'website', 'pattern', 'manual')),
    email_status     text NOT NULL DEFAULT '',
    email_confidence smallint NOT NULL DEFAULT 0 CHECK (email_confidence BETWEEN 0 AND 100),
    linkedin_url     text NOT NULL DEFAULT '',
    phone            text NOT NULL DEFAULT '',
    source           text NOT NULL CHECK (source IN ('schema', 'page', 'imprint', 'email', 'linkedin', 'manual')),
    source_url       text NOT NULL DEFAULT '',
    contact_id       uuid REFERENCES contacts (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT NOW(),
    updated_at       timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (lead_id, name_key)
);

CREATE INDEX idx_lead_people_org_lead ON lead_people (organization_id, lead_id);
CREATE INDEX idx_lead_people_contact ON lead_people (contact_id) WHERE contact_id IS NOT NULL;
