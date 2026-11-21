// /v1/linkedin: LinkedIn accounts run through GraphKit, their billing, the connect flow and lead search.
// Money is micro-USD. Account, session and search shapes are GraphKit's social contract passed through.
import type Pagination from "@/lib/api/models/app/Pagination";
import type { QuotePayment } from "@/lib/api/models/app/store/Store";

export type LinkedInMode = "none" | "managed" | "linked";
export type LinkedInTier = "standard" | "agency" | "agency_pro";
export type LinkedInBuyBlocked = "plan_required" | "partner_managed";

export interface LinkedInStatus {
    /** False when the instance has no GraphKit connection. */
    available: boolean;
    mode: LinkedInMode;
    linked_workspace: { name: string } | null;
    linked_at: Date | string | null;
    can_buy: boolean;
    buy_blocked_reason: LinkedInBuyBlocked | string | null;
    price: { monthly_micros: number; tier: LinkedInTier | string };
    acknowledgement_version: string;
    accounts: number;
}

export interface LinkedInLinkStart {
    url: string;
    expires_at: Date | string;
}

export interface LinkedInConnectQuote {
    monthly_micros: number;
    tier: LinkedInTier | string;
    due_now_micros: number;
    payment: QuotePayment;
    /** Nothing is charged: a reconnect, or a linked GraphKit workspace. */
    free: boolean;
}

// ---------------------------------------------------------------- accounts

export type LinkedInAccountStatus = "connecting" | "active" | "reconnect_required" | "restricted" | "paused" | "disconnected" | "removed";
export type LinkedInAction = "invite" | "message" | "inmail" | "profile_visit" | "search";

export interface LinkedInLimits {
    invite?: number;
    message?: number;
    inmail?: number;
    profile_visit?: number;
    search?: number;
}

export interface LinkedInCeilings extends LinkedInLimits {
    weekly_invite?: number;
}

export interface LinkedInWorkingHours {
    /** ISO weekdays, 1 Monday to 7 Sunday. */
    days: number[];
    start: string;
    end: string;
}

export interface LinkedInWarmup {
    enabled: boolean;
    percent?: number;
    ends_on?: string | null;
    weeks?: number;
    start_percent?: number;
}

export interface LinkedInUsage {
    action: LinkedInAction | string;
    today: number;
    daily_limit: number;
    week?: number;
    weekly_limit?: number | null;
}

export interface LinkedInBilling {
    mode: "store" | "linked";
    monthly_micros: number | null;
    paid_through: Date | string | null;
    unpaid_since: Date | string | null;
}

export interface LinkedInAccount {
    id: string;
    status: LinkedInAccountStatus | string;
    status_reason: string | null;
    display_name: string;
    headline: string;
    avatar_url: string;
    profile_url: string;
    public_id?: string;
    licences: string[];
    limits: LinkedInLimits;
    weekly_limits: { invite?: number };
    limit_ceilings: LinkedInCeilings;
    working_hours: LinkedInWorkingHours | null;
    timezone: string;
    warmup: LinkedInWarmup | null;
    usage: LinkedInUsage[];
    inmail_credits?: { classic?: number | null; sales_navigator?: number | null; recruiter?: number | null } | null;
    pending_invites?: number | null;
    acceptance_rate?: number | null;
    withdraw_invites_after_days?: number | null;
    billing: LinkedInBilling;
    connected_by?: string | null;
    connected_at: Date | string | null;
    updated_at?: Date | string;
}

export interface LinkedInAccountPatch {
    limits?: LinkedInLimits;
    weekly_limits?: { invite?: number };
    working_hours?: LinkedInWorkingHours;
    timezone?: string;
    warmup?: { enabled: boolean; weeks?: number; start_percent?: number };
    withdraw_invites_after_days?: number;
}

export interface LinkedInList<T> {
    data: T[];
    pagination?: Pagination;
}

// ---------------------------------------------------------------- connect sessions

export type ConnectStatus = "sign_in" | "checkpoint" | "succeeded" | "failed" | "cancelled" | "expired";
export type CheckpointType = "two_factor" | "otp" | "otp_or_app" | "app_approval" | "phone_number" | "captcha" | "choose_contract" | "choose_method" | "unsupported";

export interface ConnectChoice {
    id: string;
    label?: string;
    name?: string;
}

export interface ConnectCheckpoint {
    type: CheckpointType | string;
    delivery?: string | null;
    choices?: (ConnectChoice | string)[];
    attempts_remaining?: number | null;
    last_error?: string | null;
    captcha_key?: string | null;
    captcha_data?: string | null;
}

export interface ConnectSession {
    id: string;
    kind: "connect" | "reconnect";
    channel?: string;
    status: ConnectStatus | string;
    checkpoint: ConnectCheckpoint | null;
    can_resend: boolean;
    account: LinkedInAccount | null;
    error: { code: string; message?: string } | null;
    expires_at: Date | string;
    created_at: Date | string;
}

export interface CreateConnection {
    account_id: string | null;
    timezone: string;
    accept: boolean;
    acknowledged: boolean;
}

export type SignIn =
    | { method: "password"; username: string; password: string; recruiter: boolean; country?: string }
    | { method: "cookie"; li_at: string; li_a?: string; recruiter: boolean };

/** details.quote on a 409 linkedin_accept_required: what connecting charges before the charge is accepted. */
export interface AcceptQuote {
    monthly_micros: number;
    currency?: string;
    billed_by?: string;
}

export type CheckpointAnswer = { code: string } | { choice: string } | { choices: string[] };

// ---------------------------------------------------------------- search

export type FinderSource = "sales_navigator_leads" | "sales_navigator_accounts" | "classic_people" | "recruiter_people";
export type FilterKind = "typeahead" | "options" | "toggle" | "text" | "range";

export interface FinderValue {
    id?: string;
    text?: string;
    exclude?: boolean;
}

export interface FinderQuery {
    source: FinderSource;
    keywords?: string;
    filters?: Record<string, FinderValue[]>;
    url?: string | null;
}

export interface FinderOption {
    id: string;
    label: string;
}

export interface FinderFilter {
    key: string;
    label: string;
    kind: FilterKind | string;
    suggest?: boolean;
    free_text?: boolean;
    options?: FinderOption[];
    exclude?: boolean;
    single?: boolean;
    min?: number;
    max?: number;
    /** False for a LinkedIn filter that cannot be run; absent means it can. */
    supported?: boolean;
    reason?: string;
    licence?: string;
    hint?: string;
    placeholder?: string;
}

export interface FinderGroup {
    key: string;
    label: string;
    filters: FinderFilter[];
}

export interface FinderCatalog {
    source: FinderSource;
    label: string;
    product?: string;
    category?: "people" | "companies" | string;
    licence?: string;
    cap: number;
    page_size?: number;
    groups: FinderGroup[];
    boolean_keywords?: boolean;
    keywords_hint?: string;
    note?: string;
}

export interface FinderPerson {
    provider_id?: string;
    public_id?: string;
    full_name: string;
    first_name?: string;
    last_name?: string;
    headline?: string;
    location?: string;
    profile_url?: string;
    picture_url?: string;
    network_distance?: "1st" | "2nd" | "3rd" | "out_of_network" | string;
    premium?: boolean;
    open_profile?: boolean;
    can_inmail?: boolean;
}

export interface FinderCompany {
    provider_id?: string;
    name: string;
    industry?: string;
    headcount?: string | number | null;
    location?: string;
    website?: string;
    linkedin_url?: string;
    logo_url?: string;
}

export interface FinderInsights {
    current_title?: string;
    current_company?: string;
    current_company_id?: string;
    position_started_on?: string;
    company_started_on?: string;
    industry?: string;
    shared_connections?: number;
    recently_hired?: boolean;
    recently_promoted?: boolean;
    posted_recently?: boolean;
    viewed_your_profile?: boolean;
    follows_company?: boolean;
    past_colleague?: boolean;
    open_to_work?: boolean;
}

export interface FinderReason {
    code: string;
    label: string;
    weight: number;
}

export interface FinderReach {
    score: number;
    level: "high" | "medium" | "low" | string;
    reasons: FinderReason[];
}

export type OutreachState = "none" | "invite_pending" | "connected" | "messaged" | "replied";

export interface WarmblyMarks {
    contact_id: string | null;
    campaigns: { id: string; name: string; status: string }[];
    outreach: OutreachState | string;
    company_contacts: number;
}

export interface FinderHit {
    key: string;
    person: FinderPerson | null;
    company: FinderCompany | null;
    insights?: FinderInsights | null;
    reach?: FinderReach | null;
    warmbly?: WarmblyMarks | null;
}

export interface FinderUsage {
    used: number;
    limit: number;
    remaining: number;
    resets_at?: Date | string | null;
}

export interface SearchPage {
    data: FinderHit[];
    next_cursor: string | null;
    total: number | null;
    cap: number;
    end_reason: null | "end" | "cap";
    account: { id: string; display_name: string } | null;
    usage: FinderUsage;
    linkedin_url?: string | null;
}

export interface AiChange {
    filter: string;
    action: "add" | "remove" | "set" | "clear";
    values: FinderValue[];
    label?: string;
}

export interface AiResult {
    query: FinderQuery;
    summary: string;
    changes: AiChange[];
    assumptions: string[];
    unresolved: (string | { text: string; label?: string })[];
    suggestions: string[];
    cost_micros: number;
}

export interface ParsedUrl {
    query: FinderQuery;
    /** Filters the URL had that cannot be shown as chips; running the URL as is keeps them. */
    skipped?: string[];
}

export interface ImportRequest {
    query: FinderQuery;
    keys?: string[];
    max?: number;
    account_id: string | null;
    segment_ids: string[];
    category_ids: string[];
    campaign_ids: string[];
    skip_existing: boolean;
    /** Runs the lead finder's person email finder for people without an email; the import then runs in the background. */
    find_emails?: boolean;
}

export interface ImportResult {
    created: number;
    updated: number;
    skipped: number;
    /** People left out because a contact needs an email and none was found. */
    no_email?: number;
    contact_ids: string[];
}

/** 202 from POST /linkedin/search/import with find_emails: the background import to follow. */
export interface ImportStarted {
    import_id: string;
}

export type LinkedInImportStatus = "queued" | "running" | "done" | "failed";

/** GET /linkedin/imports/:id: a background import's progress and, once done, its summary. */
export interface LinkedInImport {
    id: string;
    status: LinkedInImportStatus | string;
    total: number;
    processed: number;
    created: number;
    updated: number;
    skipped: number;
    no_email: number;
    emails_found: number;
    error?: string | null;
    created_at: Date | string;
    updated_at: Date | string;
}

export interface SavedSearch {
    id: string;
    name: string;
    query: FinderQuery;
    created_by?: string | null;
    created_at: Date | string;
    updated_at: Date | string;
}

export interface LinkedInProfile {
    person: FinderPerson;
    summary?: string;
    current_title?: string;
    current_company?: string;
    industry?: string;
    follower_count?: number;
    relation?: { network_distance?: string; invitation_pending?: boolean; is_following?: boolean };
    experience?: { title?: string; company?: string; started_on?: string; ended_on?: string | null }[];
}

// ---------------------------------------------------------------- conversations

export interface LinkedInParticipant {
    provider_id?: string;
    public_id?: string;
    full_name: string;
    headline?: string;
    profile_url?: string;
    picture_url?: string;
}

export type ConversationKind = "chat" | "message" | "inmail" | string;

export interface LinkedInConversation {
    id: string;
    account_id: string;
    contact_id: string | null;
    participant: LinkedInParticipant;
    kind: ConversationKind;
    unread_count: number;
    last_message_at: Date | string | null;
    last_message_preview: string;
    created_at: Date | string;
    updated_at: Date | string;
}

export type MessageDirection = "inbound" | "outbound";
export type MessageStatus = "pending" | "sent" | "failed";
export type MessageKind = "message" | "inmail" | "invite_note";

export interface LinkedInMessage {
    id: string;
    conversation_id: string;
    direction: MessageDirection | string;
    body: string;
    sent_at: Date | string | null;
    status: MessageStatus | string;
    kind: MessageKind | string;
    sent_by: string | null;
    created_at: Date | string;
}

// ---------------------------------------------------------------- contact activity

export type LinkedInActionType = "invite" | "message" | "inmail" | "profile_visit" | "follow" | "like_post" | "withdraw_invite" | string;
export type LinkedInActionStatus = "pending" | "queued" | "running" | "succeeded" | "skipped" | "failed" | "cancelled" | "expired" | string;

/** One LinkedIn step a campaign ran (or tried to run) for a contact. */
export interface LinkedInContactAction {
    id: string;
    type: LinkedInActionType;
    status: LinkedInActionStatus;
    skip_reason?: string | null;
    error_code?: string | null;
    /** Plain-language reason for a failed or skipped action, when the server words one. */
    reason?: string | null;
    account_id?: string | null;
    campaign_id?: string | null;
    campaign_name?: string | null;
    step_id?: string | null;
    queued_at?: Date | string | null;
    completed_at?: Date | string | null;
}

export interface LinkedInContactActivity {
    actions: LinkedInContactAction[];
    conversations: LinkedInConversation[];
}
