// Mirror of the backend's models/integration.go shapes. Only the fields
// the dashboard renders are typed; opaque blobs like display_fields stay
// generic so the UI can dig in without round-trips to the type system.

export type IntegrationProvider =
    | "hubspot"
    | "salesforce"
    | "pipedrive"
    | "close"
    | "zapier"
    | "make"
    | "n8n"
    | "slack"
    | "discord"
    | "calendly"
    | "cal_com"
    | "google_sheets";

export type IntegrationStatus = "pending" | "connected" | "degraded" | "disconnected";

export type IntegrationCategory =
    | "crm"
    | "automation"
    | "notifications"
    | "meetings"
    | "data";

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
