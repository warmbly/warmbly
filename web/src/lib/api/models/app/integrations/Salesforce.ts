// Mirror of the backend's native Salesforce sync shapes (models/salesforce.go
// and the salesforce settings document), plus the labels the dashboard shows.

export type SalesforceObject = "Lead" | "Contact";
export type SalesforceEnvironment = "production" | "sandbox";

export type SalesforceCreateWhen = "never" | "reply" | "send";
export type SalesforceOwnerMode = "connected_user" | "sender" | "fixed";
export type SalesforceAssignTo = "record_owner" | "sender" | "connected_user";
export type SalesforceOptOutSync = "both" | "to_salesforce" | "from_salesforce" | "off";
export type SalesforceFieldDirection = "push" | "pull" | "both";
export type SalesforceFieldPolicy = "overwrite" | "if_empty";

export interface SalesforceFieldMapRow {
    object: SalesforceObject;
    warmbly: string;
    salesforce: string;
    direction: SalesforceFieldDirection;
    policy: SalesforceFieldPolicy;
}

export interface SalesforceSettings {
    enabled: boolean;
    matching: {
        prefer: "contact" | "lead";
        create_when: SalesforceCreateWhen;
        create_as: "lead" | "contact";
        lead_source: string;
        lead_status: string;
        owner: SalesforceOwnerMode;
        owner_id?: string;
        run_assignment_rules: boolean;
    };
    activity: {
        sent: boolean;
        replied: boolean;
        opened: boolean;
        clicked: boolean;
        bounced: boolean;
        unsubscribed: boolean;
        meeting_booked: boolean;
        include_body: boolean;
        assign_to: SalesforceAssignTo;
        relate_to_opportunity: boolean;
    };
    writeback: {
        lead_status_on_sent: string;
        lead_status_on_reply: Record<string, string>;
        lead_status_on_meeting: string;
        never_move_backwards: boolean;
    };
    inbound: {
        opt_out: SalesforceOptOutSync;
        pause_on_converted: boolean;
        pause_on_statuses: string[];
        pause_on_open_opportunity: boolean;
    };
    field_map: SalesforceFieldMapRow[];
    daily_api_budget: number;
}

export interface SalesforceWarmblyField {
    key: string;
    label: string;
}

export interface SalesforceSettingsResponse {
    settings: SalesforceSettings;
    warmbly_fields: SalesforceWarmblyField[];
    // The server's defaults, for "Reset to defaults".
    defaults: SalesforceSettings;
}

export interface SalesforcePicklistValue {
    value: string;
    label: string;
}

export interface SalesforceFieldInfo {
    name: string;
    label: string;
    type: string;
    createable: boolean;
    updateable: boolean;
    calculated: boolean;
    custom: boolean;
    picklist?: SalesforcePicklistValue[];
}

export interface SalesforceMetadata {
    lead_fields: SalesforceFieldInfo[];
    contact_fields: SalesforceFieldInfo[];
    lead_statuses: SalesforcePicklistValue[];
    lead_sources: SalesforcePicklistValue[];
}

export interface SalesforceCheck {
    key: string;
    label: string;
    ok: boolean;
    detail?: string;
}

export interface SalesforceOverview {
    connection_id: string;
    label: string;
    status: string;
    health: string;
    health_detail?: string;
    org: {
        id?: string;
        instance_url: string;
        environment: SalesforceEnvironment;
        login_host: string;
        user_id?: string;
        account?: string;
    };
    api: { used: number; max: number; calls_today: number; budget: number };
    counts: { pending: number; synced_24h: number; failed: number; skipped_24h: number; linked_records: number };
    last_pull_at?: string | Date;
    last_pull_error?: string;
    settings_enabled: boolean;
    checks?: SalesforceCheck[];
}

export interface SalesforceUser {
    id: string;
    name: string;
    email: string;
}

export interface SalesforceListView {
    id: string;
    label: string;
    object: string;
}

export interface SalesforceCampaign {
    id: string;
    name: string;
    status: string;
    type: string;
    member_count: number;
}

export type SalesforceSourceKind = "list_view" | "campaign";
export type SalesforceImportObject = "Lead" | "Contact" | "CampaignMember";

export interface SalesforceImportPreviewRow {
    record_id: string;
    object: string;
    name: string;
    email: string;
    company: string;
    title: string;
    owner_name: string;
    status: string;
    already_linked: boolean;
}

export interface SalesforceImportPreview {
    total: number;
    sample: SalesforceImportPreviewRow[];
}

export interface SalesforceRunResult {
    read: number;
    imported: number;
    updated: number;
    linked: number;
    skipped: number;
    failed: number;
    no_email: number;
    // Email Opt Out set in Salesforce: suppressed instead of imported.
    opted_out?: number;
    truncated?: boolean;
}

export interface SalesforceImportSource {
    id: string;
    connection_id: string;
    name: string;
    source_kind: SalesforceSourceKind;
    object: SalesforceImportObject;
    source_id: string;
    source_label: string;
    campaign_id?: string | null;
    category_ids: string[] | null;
    segment_ids: string[] | null;
    recurring: boolean;
    enabled: boolean;
    status: "idle" | "running" | "error";
    last_run_at?: string | Date | null;
    last_result?: SalesforceRunResult | null;
    last_error?: string;
    total_imported: number;
    created_at: string | Date;
    updated_at: string | Date;
}

export interface CreateSalesforceImportSourceInput {
    name?: string;
    source_kind: SalesforceSourceKind;
    object: SalesforceImportObject;
    source_id: string;
    source_label: string;
    campaign_id?: string;
    category_ids?: string[];
    segment_ids?: string[];
    recurring: boolean;
}

export interface UpdateSalesforceImportSourceInput {
    name?: string;
    campaign_id?: string | null;
    category_ids?: string[];
    segment_ids?: string[];
    recurring?: boolean;
    enabled?: boolean;
}

export type SalesforceActivityKind =
    | "sent"
    | "opened"
    | "clicked"
    | "replied"
    | "bounced"
    | "unsubscribed"
    | "meeting_booked";

export type SalesforceActivityStatus = "pending" | "synced" | "skipped" | "failed";

export interface SalesforceActivity {
    id: string;
    connection_id: string;
    contact_id?: string;
    contact_email: string;
    kind: SalesforceActivityKind;
    payload?: Record<string, unknown>;
    status: SalesforceActivityStatus;
    attempts: number;
    next_attempt_at: string | Date;
    record_id?: string;
    task_id?: string;
    detail?: string;
    occurred_at: string | Date;
    created_at: string | Date;
    processed_at?: string | Date | null;
}

export interface SalesforceActivityPage {
    data: SalesforceActivity[];
    pagination: { next_cursor?: string | null; has_more: boolean };
}

export interface ContactSalesforceRecord {
    link_id: string;
    connection_id: string;
    connection_label: string;
    object: SalesforceObject;
    id: string;
    url: string;
    name: string;
    title?: string;
    company?: string;
    email?: string;
    phone?: string;
    status?: string;
    owner?: { id: string; name: string };
    account?: { id: string; name: string; url: string };
    is_converted: boolean;
    opted_out: boolean;
    lead_source?: string;
    opportunities: {
        id: string;
        name: string;
        stage: string;
        amount?: number;
        close_date?: string;
        is_closed: boolean;
        is_won: boolean;
        url: string;
    }[];
    tasks: {
        id: string;
        subject: string;
        date?: string;
        status: string;
        owner_name?: string;
        url: string;
        from_warmbly: boolean;
    }[];
    linked_by: "match" | "created" | "import" | "manual";
    last_synced_at?: string | Date;
    last_pushed_at?: string | Date;
    sync: { pending: number; failed: number; synced: number; last_error?: string };
    stale: boolean;
    error?: string;
}

export interface ContactSalesforcePanel {
    connections: { id: string; label: string; environment: string; instance_url: string }[];
    records: ContactSalesforceRecord[];
    can_sync: boolean;
}

// --- presentation -----------------------------------------------------------

export const SALESFORCE_ACTIVITY_LABELS: Record<SalesforceActivityKind, string> = {
    sent: "Email sent",
    replied: "Reply",
    opened: "Opened",
    clicked: "Clicked",
    bounced: "Bounced",
    unsubscribed: "Unsubscribed",
    meeting_booked: "Meeting booked",
};

// Order the activity toggles are listed in Sync rules.
export const SALESFORCE_ACTIVITY_KINDS: SalesforceActivityKind[] = [
    "sent",
    "replied",
    "meeting_booked",
    "bounced",
    "unsubscribed",
    "opened",
    "clicked",
];

// Reply intents a lead status can be written for. "any" is the fallback.
export const SALESFORCE_REPLY_INTENTS: { value: string; label: string }[] = [
    { value: "any", label: "Any reply" },
    { value: "positive", label: "Positive" },
    { value: "question", label: "Question" },
    { value: "neutral", label: "Neutral" },
    { value: "negative", label: "Negative" },
    { value: "out_of_office", label: "Out of office" },
];

export const SALESFORCE_DIRECTION_LABELS: Record<SalesforceFieldDirection, string> = {
    push: "Warmbly → Salesforce",
    pull: "Salesforce → Warmbly",
    both: "Two-way",
};

export const SALESFORCE_POLICY_LABELS: Record<SalesforceFieldPolicy, string> = {
    overwrite: "Always overwrite",
    if_empty: "Only fill blanks",
};

export const SALESFORCE_IMPORT_OBJECT_LABELS: Record<SalesforceImportObject, string> = {
    Lead: "Leads",
    Contact: "Contacts",
    CampaignMember: "Campaign members",
};

// The canonical Salesforce link for a record or task id on an instance.
export function salesforceRecordURL(instanceURL: string | undefined, id: string | undefined): string | null {
    if (!instanceURL || !id) return null;
    return `${instanceURL.replace(/\/+$/, "")}/${id}`;
}
