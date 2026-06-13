// Mirror of the backend's models/integration.go shapes. Only the fields the
// dashboard renders are typed; opaque blobs like display_fields / config stay
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
    | "cal_com";

export type IntegrationAuthMethod = "oauth" | "api_key" | "webhook";

export type IntegrationStatus =
    | "pending"
    | "authorizing"
    | "connected"
    | "degraded"
    | "reauth_required"
    | "disconnected";

export type IntegrationHealth = "unknown" | "healthy" | "degraded" | "down";

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
    auth_method: IntegrationAuthMethod;
    badge_color?: string;
    beta: boolean;
    webhook_hint?: string;
    highlights?: string[];
    scopes?: string[];
    events?: string[];
    /** Provider action identifiers that have a real backend handler. The
     *  automation builder is only offered when this is non-empty. */
    action_types?: string[];
    /** Whether this provider can be a target of the contextual "push contacts"
     *  action (CRM providers with an upsert handler). */
    supports_push: boolean;
    /** Configurable-action descriptor the onboarding + field-mapping UI renders
     *  from. Absent for providers with no descriptor. */
    capability?: ProviderCapability;
    /** Whether the server has OAuth client credentials wired for this provider. */
    configured: boolean;
}

export type SyncDirection = "push" | "pull" | "both";

export interface FieldDef {
    key: string;
    label: string;
}

export interface CapabilityObject {
    name: string;
    label: string;
    dedupe_keys: string[];
    required: string[];
    warmbly_fields: FieldDef[];
    external_fields: FieldDef[];
    dynamic_fields: boolean;
}

export interface CapabilityAction {
    id: IntegrationAction;
    label: string;
    description: string;
    object?: string;
    needs_pipeline?: boolean;
    needs_channel?: boolean;
    needs_url?: boolean;
}

export interface CapabilityPicker {
    key: string;
    label: string;
    endpoint?: string;
    depends_on?: string;
}

export interface ProviderCapability {
    provider: IntegrationProvider;
    directions: SyncDirection[];
    objects?: CapabilityObject[];
    actions?: CapabilityAction[];
    pickers?: CapabilityPicker[];
    supports_booking_link: boolean;
}

export type FieldTransform = "none" | "static" | "uppercase" | "lowercase" | "trim";

export interface IntegrationFieldMapping {
    id: string;
    connection_id: string;
    organization_id: string;
    subscription_id?: string | null;
    direction: string;
    object_name: string;
    warmbly_field: string;
    external_field: string;
    transform: string;
    static_value: string;
    is_default: boolean;
    created_at: string;
}

export interface IntegrationConnection {
    id: string;
    organization_id: string;
    provider: IntegrationProvider;
    label: string;
    status: IntegrationStatus;
    auth_method: IntegrationAuthMethod;
    display_fields: Record<string, unknown>;
    connected_by_user_id?: string | null;
    external_account_id?: string;
    external_account_name?: string;
    granted_scopes?: string[];
    token_expires_at?: string | null;
    health: IntegrationHealth;
    health_detail?: string | null;
    health_checked_at?: string | null;
    last_synced_at?: string | null;
    last_error?: string | null;
    last_error_at?: string | null;
    created_at: string;
    updated_at: string;

    /** Per-connection onboarding/capability snapshot (selected use-cases, picker
     *  selections, scheduling_url for meeting providers). */
    config_capabilities?: Record<string, unknown>;
    /** Data-flow direction: push | pull | both. */
    sync_direction?: SyncDirection;

    /** Returned once at create time for inbound-webhook providers. */
    inbound_webhook_url?: string;
}

export type IntegrationAction =
    | "slack.notify"
    | "discord.notify"
    | "hubspot.upsert_contact"
    | "pipedrive.upsert_person"
    | "salesforce.upsert_contact"
    | "close.upsert_lead"
    | "webhook.ping"
    // Native (Warmbly built-in) automation actions — no external connection.
    | "warmbly.add_tag"
    | "warmbly.remove_tag"
    | "warmbly.create_task"
    | "warmbly.create_deal"
    | "warmbly.move_deal_stage"
    | "warmbly.unsubscribe";

export interface IntegrationEventSubscription {
    id: string;
    connection_id: string;
    organization_id: string;
    event_type: string;
    action: IntegrationAction;
    config: Record<string, unknown>;
    enabled: boolean;
    created_at: string;
    updated_at: string;
}

export interface IntegrationSyncRun {
    id: string;
    connection_id: string;
    organization_id: string;
    kind: string;
    status: "running" | "success" | "error";
    detail: string;
    records_processed: number;
    started_at: string;
    finished_at?: string | null;
}

export interface IntegrationOAuthStartResponse {
    url: string;
    state: string;
}

export interface IntegrationConnectionDetail {
    connection: IntegrationConnection;
    events: IntegrationEventSubscription[];
    runs: IntegrationSyncRun[];
}

export type MeetingStatus = "booked" | "rescheduled" | "canceled" | "completed" | "no_show";

export interface MeetingBooking {
    id: string;
    organization_id: string;
    source: "calendly" | "cal_com" | "manual";
    external_event_id: string;
    status: MeetingStatus;
    invitee_email: string;
    invitee_name: string;
    event_name: string;
    event_type?: string;
    scheduled_for?: string;
    end_time?: string;
    join_url?: string;
    location?: string;
    cancel_url?: string;
    reschedule_url?: string;
    canceled_reason?: string;
    contact_id?: string;
    campaign_id?: string;
    contact_name?: string;
    created_at: string;
    updated_at?: string;
}

export interface MeetingsSummary {
    upcoming: number;
    today: number;
    total: number;
    canceled: number;
}

export interface MeetingsPage {
    data: MeetingBooking[];
    pagination: {
        total: number;
        // Opaque cursor for the next page (offset-encoded under the hood), or null
        // on the last page. Same shape as every other list.
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface MeetingsSearch {
    timeframe?: "upcoming" | "past" | "";
    status?: MeetingStatus | "";
    q?: string;
}

// Payload for a manually-created meeting (source "manual"). The contact is
// attributed by an explicit id or, failing that, an org-scoped email match.
export interface CreateMeetingInput {
    title: string;
    invitee_name: string;
    invitee_email: string;
    scheduled_for: string; // RFC3339
    duration_minutes?: number;
    location?: string;
    join_url?: string;
    contact_id?: string;
}

// --- presentation helpers (shared by cards + drawers) ----------------------

export const CATEGORY_LABELS: Record<IntegrationCategory, string> = {
    crm: "CRM",
    automation: "Automation",
    notifications: "Notifications",
    meetings: "Meetings",
    data: "Data",
};

export const CATEGORY_ORDER: IntegrationCategory[] = [
    "crm",
    "notifications",
    "automation",
    "meetings",
    "data",
];

// Reply-intent classifier buckets, used to filter reply automations
// ("only notify me on positive replies"). Mirrors models.ReplyIntentType.
export const REPLY_INTENT_OPTIONS: { value: string; label: string }[] = [
    { value: "positive", label: "Positive" },
    { value: "question", label: "Question" },
    { value: "neutral", label: "Neutral" },
    { value: "negative", label: "Negative" },
    { value: "out_of_office", label: "Out of office" },
];

// Human labels for the Warmbly event vocabulary (subset surfaced as triggers).
export const EVENT_LABELS: Record<string, string> = {
    "campaign.reply_received": "Prospect replies",
    "campaign.email_bounced": "Email bounces",
    "campaign.unsubscribed": "Contact unsubscribes",
    "warmup.health_changed": "Warmup health changes",
    "deliverability.complaint": "Spam complaint",
    "meeting.booked": "Meeting booked",
    "meeting.rescheduled": "Meeting rescheduled",
    "meeting.canceled": "Meeting canceled",
    "campaign.action": "Launched by a campaign step",
    "inbound.webhook": "Inbound webhook",
};

// Which action a provider performs for an event subscription.
export function defaultActionForProvider(provider: IntegrationProvider): IntegrationAction {
    switch (provider) {
        case "slack":
            return "slack.notify";
        case "discord":
            return "discord.notify";
        case "hubspot":
            return "hubspot.upsert_contact";
        case "pipedrive":
            return "pipedrive.upsert_person";
        case "salesforce":
            return "salesforce.upsert_contact";
        case "close":
            return "close.upsert_lead";
        default:
            return "webhook.ping";
    }
}

// CRM providers the contextual "push to CRM" action can target.
export const PUSHABLE_PROVIDERS: IntegrationProvider[] = [
    "hubspot",
    "pipedrive",
    "salesforce",
    "close",
];

// Display names for providers, used by contextual menus that list connections.
export const PROVIDER_LABELS: Record<IntegrationProvider, string> = {
    hubspot: "HubSpot",
    salesforce: "Salesforce",
    pipedrive: "Pipedrive",
    close: "Close",
    zapier: "Zapier",
    make: "Make",
    n8n: "n8n",
    slack: "Slack",
    discord: "Discord",
    calendly: "Calendly",
    cal_com: "Cal.com",
};

// A connection is bookable when it's a connected scheduling provider with a
// stored scheduling_url. Returns the URL or null.
export function bookingURL(conn: IntegrationConnection): string | null {
    if (conn.provider !== "calendly" && conn.provider !== "cal_com") return null;
    if (conn.status !== "connected" && conn.status !== "degraded") return null;
    const fromConfig = conn.config_capabilities?.scheduling_url;
    const fromDisplay = conn.display_fields?.scheduling_url;
    const url = (typeof fromConfig === "string" && fromConfig) || (typeof fromDisplay === "string" && fromDisplay) || "";
    return /^https?:\/\//i.test(url) ? url : null;
}

// prefilledBookingURL appends Calendly/Cal.com-style email + name prefill params
// to a scheduling link so the contact's details are filled in for them. When a
// contactId is given we also embed it as utm_content: both providers echo this
// back in the booking webhook, letting us attribute the meeting to the exact
// contact even if they book with a different email.
export function prefilledBookingURL(
    base: string,
    email?: string,
    name?: string,
    contactId?: string,
): string {
    try {
        const u = new URL(base);
        if (email) u.searchParams.set("email", email);
        if (name) u.searchParams.set("name", name);
        if (contactId) u.searchParams.set("utm_content", contactId);
        return u.toString();
    } catch {
        return base;
    }
}
