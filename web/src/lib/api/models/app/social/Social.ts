// /v1/social: social accounts (LinkedIn, Instagram, WhatsApp, Telegram, Facebook) run through GraphKit, their
// billing, the connect flow, conversations, and LinkedIn lead search. Money is micro-USD. Account, session and
// search shapes are GraphKit's social contract passed through.
import type Pagination from "@/lib/api/models/app/Pagination";
import type { QuotePayment } from "@/lib/api/models/app/store/Store";

export type SocialChannel = "linkedin" | "instagram" | "whatsapp" | "telegram" | "facebook";
export type SocialMode = "none" | "managed" | "linked";
export type SocialTier = "standard" | "agency" | "agency_pro";
export type SocialBuyBlocked = "plan_required" | "partner_managed";

export interface SocialStatus {
    /** False when the instance has no GraphKit connection. */
    available: boolean;
    mode: SocialMode;
    linked_workspace: { name: string } | null;
    linked_at: Date | string | null;
    can_buy: boolean;
    buy_blocked_reason: SocialBuyBlocked | string | null;
    /** One price per account, whatever the channel. */
    price: { monthly_micros: number; tier: SocialTier | string };
    acknowledgement_version: string;
    accounts: number;
    /** False on an instance that charges nothing for seats. */
    metered?: boolean;
}

export interface SocialLinkStart {
    url: string;
    expires_at: Date | string;
}

export interface SocialConnectQuote {
    monthly_micros: number;
    tier: SocialTier | string;
    due_now_micros: number;
    payment: QuotePayment;
    /** Nothing is charged: a reconnect, a linked GraphKit workspace, or an instance that charges nothing. */
    free: boolean;
}

// ---------------------------------------------------------------- catalog

export type AuthMethod = "password" | "cookie" | "qr" | "pairing_code";
export type ConsentBasis = "opt_in" | "existing_conversation" | "imported_with_consent" | "friend";

/** One channel as GET /social/channels describes it: what it can do and the limits its text is held to. */
export interface ChannelSpec {
    channel: SocialChannel;
    label: string;
    auth_methods: (AuthMethod | string)[];
    cookie_names: { required: string[]; optional: string[] } | null;
    actions: (SocialActionType | string)[];
    signals: string[];
    consent_required: boolean;
    consent_bases: (ConsentBasis | string)[];
    message_max_chars: number;
    comment_max_chars: number;
    voice_note_max_seconds: number;
    invite_note_max_chars: number;
    reactions: string[];
    /** Absent from an older backend; the step editor falls back to the shared defaults. */
    voice_script_max_chars?: number;
    inmail_subject_max_chars?: number;
    inmail_body_max_chars?: number;
    max_post_age_days?: number;
}

// ---------------------------------------------------------------- accounts

export type SocialAccountStatus = "connecting" | "active" | "reconnect_required" | "restricted" | "paused" | "disconnected" | "removed";
export type SocialActionType =
    | "invite"
    | "message"
    | "inmail"
    | "voice_note"
    | "profile_visit"
    | "follow"
    | "like_post"
    | "comment_post"
    | "endorse"
    | "withdraw_invite";

/** Per-action limits keyed by action type; only the channel's actions appear. */
export type ActionLimits = Partial<Record<SocialActionType | "search" | string, number>>;

export interface SocialWorkingHours {
    /** ISO weekdays, 1 Monday to 7 Sunday. */
    days: number[];
    start: string;
    end: string;
}

export interface SocialWarmup {
    enabled: boolean;
    percent?: number;
    ends_on?: string | null;
    weeks?: number;
    start_percent?: number;
}

export interface SocialUsage {
    action: SocialActionType | string;
    today: number;
    daily_limit: number;
    week?: number;
    weekly_limit?: number | null;
}

export interface SocialBilling {
    mode: "store" | "linked";
    monthly_micros: number | null;
    paid_through: Date | string | null;
    unpaid_since: Date | string | null;
}

export interface SocialCapability {
    supported: boolean;
    reason: string | null;
}

export interface SocialAccount {
    id: string;
    channel: SocialChannel;
    status: SocialAccountStatus | string;
    status_reason: string | null;
    display_name: string;
    /** The handle on the channel: a username, a phone number, a profile id. */
    username?: string | null;
    headline: string;
    avatar_url: string;
    profile_url: string;
    public_id?: string;
    /** LinkedIn licences; empty elsewhere. */
    licences: string[];
    limits: ActionLimits;
    weekly_limits: ActionLimits;
    limit_ceilings: ActionLimits;
    weekly_limit_ceilings?: ActionLimits;
    capabilities?: Partial<Record<SocialActionType | string, SocialCapability>>;
    voice?: { available: boolean } | null;
    working_hours: SocialWorkingHours | null;
    timezone: string;
    warmup: SocialWarmup | null;
    usage: SocialUsage[];
    /** LinkedIn only. */
    inmail_credits?: { classic?: number | null; sales_navigator?: number | null; recruiter?: number | null } | null;
    pending_invites?: number | null;
    acceptance_rate?: number | null;
    withdraw_invites_after_days?: number | null;
    billing: SocialBilling;
    connected_by?: string | null;
    connected_at: Date | string | null;
    updated_at?: Date | string;
}

export interface SocialAccountPatch {
    limits?: ActionLimits;
    weekly_limits?: ActionLimits;
    working_hours?: SocialWorkingHours;
    timezone?: string;
    warmup?: { enabled: boolean; weeks?: number; start_percent?: number };
    withdraw_invites_after_days?: number;
}

export interface SocialList<T> {
    data: T[];
    pagination?: Pagination;
}

/** POST /social/audio: a stored recording a voice note step sends. */
export interface SocialAudio {
    id: string;
    duration_seconds: number;
    content_type: string;
    created_at: Date | string;
}

// ---------------------------------------------------------------- connect sessions

export type ConnectStatus = "sign_in" | "checkpoint" | "succeeded" | "failed" | "cancelled" | "expired";
export type CheckpointType =
    | "two_factor"
    | "otp"
    | "otp_or_app"
    | "app_approval"
    | "phone_number"
    | "captcha"
    | "choose_contract"
    | "choose_method"
    | "qr_code"
    | "pairing_code"
    | "loading"
    | "encrypted_history"
    | "unsupported";

export interface ConnectChoice {
    id: string;
    label?: string;
    name?: string;
    attempts_remaining?: number | null;
}

export interface ConnectCheckpoint {
    type: CheckpointType | string;
    delivery?: string | null;
    choices?: (ConnectChoice | string)[];
    attempts_remaining?: number | null;
    last_error?: string | null;
    captcha_key?: string | null;
    captcha_data?: string | null;
    /** qr_code: the payload to render as a QR code (not an image). It rotates, so the session is polled. */
    qr_code?: string | null;
    /** pairing_code: the code the user types into WhatsApp. */
    pairing_code?: string | null;
    expires_at?: Date | string | null;
}

export interface ConnectSession {
    id: string;
    kind: "connect" | "reconnect";
    channel: SocialChannel;
    method?: AuthMethod | string | null;
    status: ConnectStatus | string;
    checkpoint: ConnectCheckpoint | null;
    can_resend: boolean;
    account: SocialAccount | null;
    error: { code: string; message?: string } | null;
    expires_at: Date | string;
    created_at: Date | string;
}

export interface CreateConnection {
    channel: SocialChannel;
    account_id: string | null;
    timezone: string;
    accept: boolean;
    acknowledged: boolean;
}

/** POST /social/connections/:id/sign-in. Only the method's own fields are sent; the server refuses the rest. */
export type SignIn =
    | { method: "password"; username: string; password: string; recruiter?: boolean }
    | { method: "cookie"; cookies: Record<string, string>; recruiter?: boolean }
    | { method: "qr" }
    | { method: "pairing_code"; phone_number: string };

/** details.quote on a 409 social_accept_required: what connecting charges before the charge is accepted. */
export interface AcceptQuote {
    monthly_micros: number;
    currency?: string;
    billed_by?: string;
}

export type CheckpointAnswer =
    | { code: string }
    | { choice: string }
    | { choices: string[] }
    | { two_factor_password: string }
    // encrypted_history: how to unlock the encrypted chat history, and the PIN or code for it.
    | { choice: string; secret: string };

// ---------------------------------------------------------------- LinkedIn search

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

/** 202 from POST /social/search/import with find_emails: the background import to follow. */
export interface ImportStarted {
    import_id: string;
}

export type LinkedInImportStatus = "queued" | "running" | "done" | "failed";

/** GET /social/imports/:id: a background import's progress and, once done, its summary. */
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

export interface SocialParticipant {
    provider_id?: string;
    public_id?: string;
    full_name: string;
    headline?: string;
    profile_url?: string;
    picture_url?: string;
    /** The participant's handle on the channel (username or phone), where there is one. */
    handle?: string;
}

export type ConversationKind = "chat" | "message" | "inmail" | string;

export interface SocialConversation {
    id: string;
    account_id: string;
    channel: SocialChannel;
    contact_id: string | null;
    participant: SocialParticipant;
    kind: ConversationKind;
    unread_count: number;
    last_message_at: Date | string | null;
    last_message_preview: string;
    created_at: Date | string;
    updated_at: Date | string;
}

export type MessageDirection = "inbound" | "outbound";
export type MessageStatus = "pending" | "sent" | "failed";
export type MessageKind = "message" | "inmail" | "invite_note" | "voice_note";

export interface SocialMessage {
    id: string;
    conversation_id: string;
    channel?: SocialChannel;
    direction: MessageDirection | string;
    body: string;
    sent_at: Date | string | null;
    status: MessageStatus | string;
    kind: MessageKind | string;
    sent_by: string | null;
    created_at: Date | string;
}

// ---------------------------------------------------------------- contact identities and activity

/** One channel's handle on a contact (contact read and write `social`). */
export interface ContactSocialIdentity {
    handle: string;
    consent_basis: ConsentBasis | string | null;
    consent_recorded_at?: Date | string | null;
}

export type ContactSocial = Partial<Record<SocialChannel, ContactSocialIdentity>>;

/** PATCH shape: a channel set to null clears it; absent channels are left alone. */
export type ContactSocialPatch = Partial<Record<SocialChannel, { handle: string; consent_basis: ConsentBasis | string | null } | null>>;

export type SocialActionStatus = "pending" | "queued" | "running" | "succeeded" | "skipped" | "failed" | "cancelled" | "expired" | string;

/** One social step a campaign ran (or tried to run) for a contact. */
export interface SocialContactAction {
    id: string;
    channel: SocialChannel;
    type: SocialActionType | string;
    status: SocialActionStatus;
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

export interface SocialContactActivity {
    actions: SocialContactAction[];
    conversations: SocialConversation[];
}
