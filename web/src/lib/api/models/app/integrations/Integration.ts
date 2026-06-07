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
    /** Whether the server has OAuth client credentials wired for this provider. */
    configured: boolean;
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
    | "webhook.ping";

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
