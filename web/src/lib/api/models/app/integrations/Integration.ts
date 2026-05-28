// Mirror of the backend's models/integration.go shapes. Only the fields
// the dashboard renders are typed — opaque blobs like display_fields stay
// generic so the UI can dig in without round-trips to the type system.

export type IntegrationProvider =
    | "calendly"
    | "cal_com"
    | "google_sheets"
    | "google_postmaster"
    | "microsoft_snds"
    | "dmarc"
    | "cloudflare"
    | "godaddy"
    | "namecheap";

export type IntegrationStatus = "pending" | "connected" | "degraded" | "disconnected";

export type IntegrationCategory =
    | "meetings"
    | "data"
    | "deliverability"
    | "dns";

export interface IntegrationCatalogEntry {
    provider: IntegrationProvider;
    name: string;
    tagline: string;
    category: IntegrationCategory;
    docs_url?: string;
    auth_method: "oauth" | "api_key" | "webhook";
    badge_color?: string;
    beta: boolean;
    webhook_hint?: string;
}

export interface IntegrationConnection {
    id: string;
    organization_id: string;
    provider: IntegrationProvider;
    label: string;
    status: IntegrationStatus;
    display_fields: Record<string, unknown>;
    last_synced_at?: string | null;
    last_error?: string | null;
    last_error_at?: string | null;
    created_at: string;
    updated_at: string;

    /** Returned once at create time for inbound-webhook providers. */
    inbound_webhook_url?: string;
}

export interface DMARCReport {
    id: string;
    organization_id: string;
    domain: string;
    reporter_org: string;
    report_id: string;
    range_start: string;
    range_end: string;
    total_messages: number;
    pass_messages: number;
    fail_messages: number;
    created_at: string;
}

export interface PostmasterSnapshot {
    id: number;
    organization_id: string;
    source: "google_postmaster" | "microsoft_snds";
    target: string;
    snapshot_date: string;
    spam_rate_pct?: number;
    inbox_placement_pct?: number;
    domain_reputation?: string;
    ip_reputation?: string;
    dkim_success_pct?: number;
    spf_success_pct?: number;
    dmarc_success_pct?: number;
    created_at: string;
}

export interface DNSVerification {
    id: string;
    organization_id: string;
    domain: string;

    spf_record?: string;
    spf_ok: boolean;

    dkim_selector?: string;
    dkim_record?: string;
    dkim_ok: boolean;

    dmarc_record?: string;
    dmarc_ok: boolean;

    tracking_cname?: string;
    tracking_ok: boolean;

    notes: Record<string, string>;
    checked_at: string;
}

export interface MeetingBooking {
    id: string;
    organization_id: string;
    source: "calendly" | "cal_com";
    external_event_id: string;
    invitee_email: string;
    invitee_name: string;
    event_name: string;
    scheduled_for?: string;
    contact_id?: string;
    campaign_id?: string;
    created_at: string;
}
