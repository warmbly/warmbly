// The lead finder: businesses found on a map, enriched from their websites.

export type LeadProvider = "osm" | "google_places" | "foursquare" | "outscraper";
export type LeadStatus = "new" | "saved" | "contacted" | "dismissed";
export type LeadEnrichmentStatus = "none" | "queued" | "done" | "no_website" | "failed";
export type LeadSort = "recent" | "score" | "rating" | "reviews" | "name";

export interface LatLng {
    lat: number;
    lng: number;
}

export interface LeadBounds {
    south: number;
    west: number;
    north: number;
    east: number;
}

export type LeadArea =
    | { kind: "bbox"; bounds: LeadBounds }
    | { kind: "circle"; bounds: LeadBounds; center: LatLng; radius_m: number }
    | { kind: "polygon"; bounds: LeadBounds; polygon: LatLng[] };

export interface LeadEmail {
    email: string;
    // "website" when read off the site, "search" when a web search showed it on a page about the business, else the provider.
    source: "website" | "search" | (string & {});
    url?: string;
    domain_match: boolean;
    status?: "valid" | "risky" | "invalid" | "unknown" | "suppressed" | "";
    sub_status?: string;
}

export interface LeadSite {
    reachable: boolean;
    final_url?: string;
    https: boolean;
    title?: string;
    description?: string;
    platform?: string;
    signals?: string[];
    phones?: string[];
    pages?: number;
    // How the domain builds a person's address, e.g. "first.last".
    email_pattern?: string;
    // The mail server accepts any address, so a worked-out one cannot be confirmed.
    accepts_all?: boolean;
    // The listing had no website; the finder worked this one out from the name and confirmed it.
    found_website?: boolean;
    // The reader service that fetched the site after it turned a direct visit away; empty when read directly.
    read_via?: LeadReaderService;
    // Why an unread site was not read; empty when it was.
    unread?: LeadSiteUnread;
    error?: string;
    checked_at?: string;
}

export type LeadSiteUnread = "blocked" | "robots" | "unreachable";

export type LeadReaderService = "jina" | "crawl4ai" | "ddgs" | "custom" | (string & {});

export type LeadSeniority = "owner" | "executive" | "manager" | "staff";
export type LeadPersonSource = "schema" | "page" | "imprint" | "email" | "linkedin" | "manual";
export type LeadPersonEmailSource = "" | "website" | "pattern" | "manual";

export interface LeadPerson {
    id: string;
    lead_id: string;
    name: string;
    first_name: string;
    last_name: string;
    title: string;
    seniority: LeadSeniority;
    email: string;
    email_source: LeadPersonEmailSource;
    // valid, risky, invalid, unknown, guessed (worked out, unconfirmed) or suppressed.
    email_status: string;
    email_confidence: number;
    linkedin_url: string;
    phone: string;
    source: LeadPersonSource;
    source_url: string;
    contact_id: string | null;
    created_at: string;
    updated_at: string;
}

export interface LeadPersonInput {
    name?: string;
    first_name?: string;
    last_name?: string;
    title?: string;
    email?: string;
    linkedin_url?: string;
    phone?: string;
}

export interface Lead {
    id: string;
    provider: LeadProvider;
    external_id: string;
    name: string;
    category: string;
    categories: string[];
    address: string;
    street: string;
    city: string;
    region: string;
    postcode: string;
    country: string;
    latitude: number;
    longitude: number;
    phone: string;
    website: string;
    domain: string;
    email: string;
    emails: LeadEmail[];
    socials: Record<string, string>;
    rating: number | null;
    review_count: number | null;
    price_level: number | null;
    hours: string[];
    business_status: string;
    maps_url: string;
    description: string;
    chain: string;
    site: LeadSite;
    score: number;
    status: LeadStatus;
    enrichment_status: LeadEnrichmentStatus;
    enriched_at: string | null;
    contact_id: string | null;
    tags: string[];
    notes: string;
    people_count: number;
    // Only on a lead read on its own.
    people?: LeadPerson[];
    first_seen_at: string;
    last_seen_at: string;
    created_at: string;
    updated_at: string;
}

export interface LeadPoint {
    id: string;
    name: string;
    category: string;
    latitude: number;
    longitude: number;
    status: LeadStatus;
    score: number;
    has_email: boolean;
    has_website: boolean;
    has_phone: boolean;
    rating: number | null;
    review_count: number | null;
    in_contacts: boolean;
}

export interface LeadSearch {
    id: string;
    created_by_user_id: string | null;
    name: string;
    provider: LeadProvider;
    query: string;
    category: string;
    location_label: string;
    area: LeadArea;
    saved: boolean;
    result_count: number;
    pages_fetched: number;
    has_more: boolean;
    last_run_at: string;
    created_at: string;
    updated_at: string;
}

export interface LeadFilter {
    search_id?: string;
    q?: string;
    statuses?: LeadStatus[];
    providers?: LeadProvider[];
    category?: string;
    tag?: string;
    has_email?: boolean;
    has_website?: boolean;
    has_phone?: boolean;
    has_socials?: boolean;
    has_people?: boolean;
    in_contacts?: boolean;
    enrichment?: LeadEnrichmentStatus[];
    min_rating?: number;
    min_reviews?: number;
    max_reviews?: number;
    min_score?: number;
    bounds?: LeadBounds;
    exclude_chains?: boolean;
}

export interface LeadSelection {
    lead_ids?: string[];
    all?: boolean;
    filter?: LeadFilter;
}

export interface LeadPatch {
    status?: LeadStatus;
    notes?: string;
    tags?: string[];
    add_tags?: string[];
    remove_tags?: string[];
}

export interface LeadProviderInfo {
    id: LeadProvider;
    name: string;
    description: string;
    available: boolean;
    key_source: "none" | "workspace" | "instance";
    needs_key: boolean;
    page_size: number;
    max_pages: number;
    /** What one page costs the balance; 0 when free or billed to the workspace's own key. */
    cost_usd: number;
    fields: string[];
    integration?: string;
}

export interface LeadCategory {
    id: string;
    label: string;
    group: string;
}

export interface LeadExportColumn {
    key: string;
    label: string;
}

export interface LeadFinderConfig {
    providers: LeadProviderInfo[];
    categories: LeadCategory[];
    default_provider: LeadProvider;
    enrich_cost_usd: number;
    person_email_cost_usd: number;
    export_columns: LeadExportColumn[];
    person_columns: LeadExportColumn[];
    default_export_columns: string[];
    map: { style_light: string; style_dark: string };
    web_reader: LeadWebFallback;
    web_search: LeadWebFallback;
}

// One way past a site that turns the finder away.
export interface LeadWebFallback {
    available: boolean;
    // Tried in order: reader or search names, "mcp", "workspace_jina", "instance".
    sources: string[];
    // What one instance search costs the balance; 0 when free.
    cost_usd: number;
}

export interface GeocodeResult {
    label: string;
    detail: string;
    kind: string;
    center: LatLng;
    bounds?: LeadBounds;
}

export interface LeadSearchRequest {
    search_id?: string;
    provider?: LeadProvider;
    query?: string;
    category?: string;
    location_label?: string;
    area?: LeadArea;
    save?: boolean;
    name?: string;
    language?: string;
}

export interface LeadSearchResult {
    search: LeadSearch;
    leads: Lead[];
    found: number;
    outside_area: number;
    charged_usd: number;
}

export interface LeadFacet {
    value: string;
    count: number;
}

export interface LeadFacets {
    categories: LeadFacet[];
    tags: LeadFacet[];
    cities: LeadFacet[];
}

export interface LeadPage {
    data: Lead[];
    pagination: { next_cursor: string | null; has_more: boolean; total: number };
}

export interface LeadEnrichRequest extends LeadSelection {
    verify?: boolean;
    people?: boolean;
    // Search the web for a missing site or a blocked site's addresses; on unless false.
    web_search?: boolean;
}

export interface LeadEnrichmentRun {
    id: string;
    requested_by_user_id: string | null;
    verify: boolean;
    people: boolean;
    web_search: boolean;
    status: "queued" | "running" | "done" | "error";
    total: number;
    processed: number;
    emails_found: number;
    people_found: number;
    no_website: number;
    failed: number;
    charged_usd: number;
    last_error: string;
    started_at: string | null;
    finished_at: string | null;
    created_at: string;
    updated_at: string;
}

export interface AddLeadsToContactsRequest extends LeadSelection {
    campaign_ids?: string[];
    category_ids?: string[];
    segment_ids?: string[];
    verified_only?: boolean;
    who?: LeadContactsWho;
    include_guessed?: boolean;
    // Leave contacts already in the workspace as they are.
    keep_existing?: boolean;
    skip_active_in_other_campaigns?: boolean;
}

export type LeadContactsWho = "business" | "people" | "both";

export interface AddLeadsToContactsResult {
    selected: number;
    imported: number;
    updated: number;
    skipped: number;
    failed: number;
    no_email: number;
    unverified: number;
    suppressed: number;
    guessed: number;
    people: number;
}

export type LeadExportFormat = "csv" | "xlsx" | "json";
export type LeadExportRows = "leads" | "people";
