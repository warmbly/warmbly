// Models for /admin/* endpoints. Trimmed to what the admin app
// currently renders — broaden as we wire more pages.

export type WorkerInstallState =
    | "pending"
    | "provisioning"
    | "installed"
    | "error"
    | "uninstalling"
    | "uninstalled";

export type WorkerType = "shared" | "dedicated";

export type WorkerRiskPool = "clean" | "risky" | "quarantine";
export type WorkerEgressKind = "cold_smtp" | "oauth_api" | "warmup_only";
export type WorkerHealthState =
    | "healthy"
    | "watch"
    | "throttled"
    | "quarantined"
    | "blocked";

export interface ManagedWorker {
    id: string;
    name: string;
    notes: string;
    ip_addr: string;
    active: boolean;
    free_tier: boolean;
    worker_type: WorkerType;
    account_count: number;
    risk_pool: WorkerRiskPool;
    egress_kind: WorkerEgressKind;
    health_state: WorkerHealthState;
    load_score: number;

    ssh_host?: string;
    ssh_port?: number;
    ssh_user?: string;
    ssh_public_key?: string;
    ssh_host_fingerprint?: string;
    install_state: WorkerInstallState;
    last_seen_at?: string;
    last_error?: string;

    profile_id?: string;
    config_applied_at?: string;
    image_version?: string;
    tags?: string[];

    created_at: string;
    updated_at: string;
}

// A mailbox assigned to a worker, with its per-mailbox health signals, returned
// by GET /admin/workers/:id/emails.
export interface AdminWorkerEmail {
    id: string;
    email: string;
    user_id: string;
    organization_id?: string | null;
    status: string;
    provider: string;
    warmup_enabled: boolean;
    last_synced_at: string;
    risk_band: string; // clean | risky | quarantine
    risk_evaluated_at?: string | null;
    warmup_health?: string; // worst warmup health_state, "" if not in a pool
    spam_score?: number | null;
    blocked_until?: string | null;
}

export interface AdminWorkerEmailsResult {
    data: AdminWorkerEmail[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface WorkerLiveStatus {
    service_active: boolean;
    container_up: boolean;
    container_image: string;
    uptime: string;
    raw: string;
}

export interface AdminAuditLog {
    id: string;
    admin_user_id: string;
    action: string;
    target_type: string;
    target_id: string;
    details?: Record<string, unknown>;
    ip_address: string;
    user_agent: string;
    created_at: string;
    admin_user?: {
        id: string;
        email: string;
        first_name?: string;
        last_name?: string;
    };
}

export interface AdminAuditLogSearch {
    admin_user_id?: string;
    action?: string;
    target_type?: string;
    target_id?: string;
    start_date?: string;
    end_date?: string;
    cursor?: string;
    limit?: number;
}

export interface AdminAuditLogsResult {
    data: AdminAuditLog[];
    pagination: {
        cursor?: string;
        has_more?: boolean;
    };
}

// /admin/analytics/overview — fields the backend returns vary, so keep
// the shape loose. The Overview page renders whatever counters come
// back and falls back to "—" for anything missing.
export interface PlatformOverview {
    users_total?: number;
    users_active_30d?: number;
    organizations_total?: number;
    workers_total?: number;
    workers_active?: number;
    workers_offline?: number;
    mailboxes_total?: number;
    mailboxes_connected?: number;
    emails_sent_24h?: number;
    emails_sent_7d?: number;
    bounce_rate_7d?: number;
    complaint_rate_7d?: number;
    campaigns_running?: number;
    [k: string]: unknown;
}

// /admin/mailboxes — cross-org mailbox admin.

export interface AdminMailboxRow {
    id: string;
    email: string;
    provider: string;
    status: string;
    user_id: string;
    owner_email: string;
    organization_id?: string | null;
    org_name?: string | null;
    worker_id?: string | null;
    warmup_enabled: boolean;
    risk_band: string;
    warmup_pool_type?: string | null;
    campaign_limit: number;
    last_synced_at?: string | null;
    created_at: string;
}

export interface AdminMailboxesResult {
    data: AdminMailboxRow[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface AdminMailboxSearch {
    q?: string;
    status?: string;
    provider?: string;
    warmup?: "on" | "off" | "";
    created_within?: number;
    org_id?: string;
    // Ownership / placement
    user_id?: string;
    worker_id?: string;
    // Classification
    risk_band?: string;
    warmup_pool_type?: string;
    synced_status?: "never" | "stale" | "recent" | "";
    // Flags
    warmup_paused?: boolean;
    tracking_domain_verified?: boolean;
    has_tracking_domain?: boolean;
    has_organization?: boolean;
    signature_sync?: boolean;
    has_oauth?: boolean;
    has_smtp_imap?: boolean;
    // Numeric ranges
    campaign_limit_min?: number;
    campaign_limit_max?: number;
    min_wait_time_min?: number;
    min_wait_time_max?: number;
    // Date ranges (YYYY-MM-DD)
    created_after?: string;
    created_before?: string;
    last_synced_after?: string;
    last_synced_before?: string;
    cursor?: string;
    limit?: number;
    sort_by?: "email" | "created_at" | "last_synced_at" | "campaign_limit";
    sort_desc?: boolean;
}

// /admin/analytics — time-series counters for the platform.

export interface DailyEmailStat {
    date: string;
    total_sent: number;
    total_delivered: number;
    total_bounced: number;
    total_opened: number;
    total_clicked: number;
    total_replied: number;
}

export interface HourlyEmailStat {
    hour: number;
    total_sent: number;
}

export interface WorkerLoadStat {
    worker_id: string;
    worker_name: string;
    emails_sent_today: number;
    queued_emails: number;
    connected_emails: number;
    cpu_usage?: number;
    memory_usage?: number;
}

export interface UserGrowthStat {
    date: string;
    new_users: number;
    total_users: number;
}

export interface AnalyticsTrends {
    users_growth_percent: number;
    emails_growth_percent: number;
    campaigns_growth_percent: number;
    revenue_growth_percent: number;
}

// /admin/outreach — platform-mailer composer (send from noreply@warmbly.com
// with configurable Reply-To, every send audit-logged).

export type AdminOutreachStatus = "queued" | "sent" | "failed";

export interface AdminOutreachMessage {
    id: string;
    sent_by: string;
    to_user_id?: string | null;
    to_org_id?: string | null;
    to_email: string;
    reply_to?: string | null;
    subject: string;
    body: string;
    status: AdminOutreachStatus;
    error?: string | null;
    sent_at?: string | null;
    created_at: string;
    sent_by_user?: AdminUserSummary;
    to_user?: AdminUserSummary;
}

export interface SendAdminOutreachRequest {
    to_user_id?: string;
    to_org_id?: string;
    to_email?: string;
    reply_to?: string;
    subject: string;
    body: string;
}

export interface AdminOutreachSearch {
    q?: string;
    status?: AdminOutreachStatus | "";
    recipient_type?: "user" | "org" | "email" | "";
    sent_by_q?: string;
    has_reply_to?: boolean;
    has_error?: boolean;
    has_user?: boolean;
    has_org?: boolean;
    // Date ranges
    created_within?: number; // days; omit for any
    created_after?: string; // YYYY-MM-DD
    created_before?: string; // YYYY-MM-DD
    sent_at_after?: string; // YYYY-MM-DD
    sent_at_before?: string; // YYYY-MM-DD
    cursor?: string;
    limit?: number;
    sort_by?: "created_at" | "sent_at" | "status" | "to_email" | "subject";
    sort_desc?: boolean;
}

export interface AdminOutreachResult {
    data: AdminOutreachMessage[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

// /admin/limit-requests — limit-increase request queue.

export type LimitRequestStatus =
    | "pending"
    | "approved"
    | "rejected"
    | "cancelled";

export interface LimitIncreaseRequest {
    id: string;
    organization_id: string;
    field: string;
    current_effective: number;
    requested: number;
    reason: string;
    status: LimitRequestStatus;
    submitted_by: string;
    submitted_at: string;
    reviewed_by?: string | null;
    reviewed_at?: string | null;
    review_notes: string;
    organization?: {
        id: string;
        name: string;
        slug?: string | null;
    };
    submitted_by_user?: {
        id: string;
        first_name: string;
        last_name: string;
        email: string;
    };
}

export interface AdminLimitRequestsResult {
    data: LimitIncreaseRequest[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface AdminLimitRequestSearch {
    q?: string;
    status?: LimitRequestStatus | "all" | "";
    field?: string; // one of the 6 app-validated field keys, or "" for any
    org_id?: string;
    submitted_by?: string;
    // Flags
    reviewed?: boolean;
    unreviewed?: boolean;
    // Numeric ranges
    requested_min?: number;
    requested_max?: number;
    current_effective_min?: number;
    current_effective_max?: number;
    // Date ranges
    submitted_within?: number; // days; omit for any
    submitted_after?: string; // YYYY-MM-DD
    submitted_before?: string; // YYYY-MM-DD
    reviewed_after?: string; // YYYY-MM-DD
    reviewed_before?: string; // YYYY-MM-DD
    cursor?: string;
    limit?: number;
    sort_by?:
        | "submitted_at"
        | "requested"
        | "current_effective"
        | "reviewed_at"
        | "status"
        | "field"
        | "org_name";
    sort_desc?: boolean;
}

// /admin/plans — plan catalog and custom-plan management.

export interface Plan {
    id: string;
    name?: string | null;
    max_contacts: number;
    daily_emails: number;
    ai_generation: boolean;
    account_limit: number;
    price: number;
    discounted_price: number;
    duration: { id: string; title: string } | string;
    savings: number;
    public: boolean;
    stripe_price_id?: string | null;
    stripe_product_id?: string | null;
    dedicated_workers: number;
    daily_campaign_limit?: number | null;
    max_campaigns?: number | null;
    max_active_campaigns?: number | null;
    max_team_members?: number | null;
    max_email_accounts?: number | null;
    updated_at: string;
    created_at: string;
}

export interface UpdatePlanRequest {
    name?: string;
    max_contacts?: number;
    daily_emails?: number;
    ai_generation?: boolean;
    account_limit?: number;
    price?: number;
    discounted_price?: number;
    dedicated_workers?: number;
    daily_campaign_limit?: number;
    max_campaigns?: number;
    max_active_campaigns?: number;
    max_team_members?: number;
    max_email_accounts?: number;
    public?: boolean;
}

export interface AdminPlanSearch {
    q?: string;
    visibility?: "public" | "private" | "";
    duration?: string; // "month" | "year"; "" = any
    ai_generation?: boolean;
    has_stripe?: boolean;
    has_subscribers?: boolean;
    // Numeric ranges
    price_min?: number;
    price_max?: number;
    daily_emails_min?: number;
    daily_emails_max?: number;
    account_limit_min?: number;
    account_limit_max?: number;
    // Date range
    created_within?: number;
    created_after?: string;
    created_before?: string;
    cursor?: string;
    limit?: number;
    sort_by?: "price" | "name" | "daily_emails" | "account_limit" | "created_at";
    sort_desc?: boolean;
}

export interface AdminPlansResult {
    data: Plan[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

// /admin/campaigns/* — platform-wide campaign admin (force-stop runaway
// campaigns, inspect engagement counters per campaign).

export interface AdminCampaignDetail {
    id: string;
    name: string;
    user_id: string;
    organization_id: string;
    status: string;
    created_at: string;
    started_at?: string | null;
    stopped_at?: string | null;
    total_contacts: number;
    emails_sent: number;
    emails_opened: number;
    emails_clicked: number;
    emails_replied: number;
    emails_bounced: number;
    user?: AdminUserSummary;
    organization?: {
        id: string;
        name: string;
        slug?: string | null;
    };
}

export interface AdminCampaignsResult {
    data: AdminCampaignDetail[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface AdminCampaignSearch {
    q?: string;
    user_id?: string;
    org_id?: string;
    status?: string; // "" | draft | active | paused | completed | paused_trial_expired | paused_no_accounts
    // Boolean flags
    open_tracking?: boolean;
    link_tracking?: boolean;
    stop_on_reply?: boolean;
    text_only?: boolean;
    unsubscribe_header?: boolean;
    // Relationship existence
    has_contacts?: boolean;
    has_bounces?: boolean;
    // Count ranges
    daily_limit_min?: number;
    daily_limit_max?: number;
    contact_count_min?: number;
    contact_count_max?: number;
    sent_count_min?: number;
    sent_count_max?: number;
    // Date ranges
    created_within?: number;
    created_after?: string;
    created_before?: string;
    start_date_after?: string;
    start_date_before?: string;
    updated_after?: string;
    updated_before?: string;
    // Pagination + sort
    cursor?: string;
    limit?: number;
    sort_by?:
        | "created_at"
        | "name"
        | "status"
        | "updated_at"
        | "daily_limit"
        | "owner_email"
        | "contact_count"
        | "sent_count";
    sort_desc?: boolean;
}

// /admin/warmup/* — warmup pool admin (the platform's most critical
// safety surface — keeps shared paid pools clean by quarantining
// risky mailboxes early).

export type WarmupAppealStatus = "pending" | "approved" | "rejected";

export interface WarmupPoolHealthSummary {
    total_participants: number;
    by_state: Record<string, number>;
    avg_spam_score: number;
    avg_spam_placement_rate: number;
    spam_placement_by_provider: Record<string, number>;
    blocked_count: number;
    at_risk_count: number;
}

export interface WarmupPoolInfo {
    type: string;
    total_participants: number;
    active_participants: number;
    blocked_count: number;
}

export interface AdminBlockedAccount {
    id: string;
    email: string;
    user_id: string;
    blocked_at: string;
    blocked_by?: string | null;
    block_reason: string;
    has_appeal: boolean;
    appeal_status?: WarmupAppealStatus | null;
    user?: AdminUserSummary;
}

export interface WarmupAppeal {
    id: string;
    email_account_id: string;
    user_id: string;
    reason: string;
    status: WarmupAppealStatus;
    reviewed_by?: string | null;
    reviewed_at?: string | null;
    review_notes?: string | null;
    created_at: string;
    user?: AdminUserSummary;
    email_account?: {
        id: string;
        email: string;
    };
    reviewed_by_user?: AdminUserSummary;
}

export interface WarmupAppealsResult {
    data: WarmupAppeal[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface AdminBlockedAccountsResult {
    data: AdminBlockedAccount[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface BlockAccountRequest {
    reason: string;
}

export interface ReviewAppealRequest {
    approved: boolean;
    notes: string;
}

// /admin/users/* — platform user admin.

export interface AdminUserSummary {
    id: string;
    first_name: string;
    last_name: string;
    email: string;
}

export interface AdminUserDetail {
    id: string;
    first_name: string;
    last_name: string;
    email: string;
    max_organizations: number;
    free_trial_used: boolean;
    admin_permissions: number;
    admin_granted_at?: string | null;
    admin_granted_by?: string | null;
    banned_at?: string | null;
    created_at: string;
    updated_at: string;
    organization_count: number;
    email_account_count: number;
    campaign_count: number;
}

export interface AdminUsersResult {
    data: AdminUserDetail[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface AdminUserSearchParams {
    q?: string;
    status?: "active" | "banned" | "all" | "";
    is_admin?: boolean;
    has_overrides?: boolean;
    free_trial_used?: boolean;
    created_within?: number;
    // Plan / subscription
    plan_id?: string;
    subscription_status?: string;
    is_enterprise?: boolean;
    has_subscription?: boolean;
    has_active_subscription?: boolean;
    // Account state
    onboarding_completed?: boolean;
    deletion_scheduled?: boolean;
    has_avatar?: boolean;
    has_active_campaign?: boolean;
    has_ban_record?: boolean;
    has_dedicated_worker?: boolean;
    // Count ranges
    org_count_min?: number;
    org_count_max?: number;
    email_account_count_min?: number;
    email_account_count_max?: number;
    campaign_count_min?: number;
    campaign_count_max?: number;
    max_organizations_min?: number;
    max_organizations_max?: number;
    // Date ranges (YYYY-MM-DD)
    created_after?: string;
    created_before?: string;
    admin_granted_after?: string;
    admin_granted_before?: string;
    banned_after?: string;
    banned_before?: string;
    updated_after?: string;
    updated_before?: string;
    cursor?: string;
    limit?: number;
    sort_by?: "created_at" | "email" | "name";
    sort_desc?: boolean;
}

export interface UserBan {
    id: string;
    user_id: string;
    banned_by: string;
    reason: string;
    banned_at: string;
    unbanned_at?: string | null;
    unbanned_by?: string | null;
    unban_reason?: string | null;
    banned_by_user?: AdminUserSummary;
    unbanned_by_user?: AdminUserSummary | null;
}

export interface AdminUserRateLimits {
    user_id: string;
    limit_ws_message_pm?: number | null;
    limit_ws_join_pm?: number | null;
    limit_ws_event_pm?: number | null;
    max_connections?: number | null;
    daily_email_limit?: number | null;
    updated_at: string;
}

export interface UpdateUserRateLimitsRequest {
    limit_ws_message_pm?: number;
    limit_ws_join_pm?: number;
    limit_ws_event_pm?: number;
    max_connections?: number;
    daily_email_limit?: number;
}

export interface BanUserRequest {
    reason: string;
    // Bitmask of ban-scope flags. 1 = login, 2 = org-create, 4 = send.
    // 0 falls back to "login only" server-side. See
    // internal/models/admin.go BanScope.
    scope?: number;
}

export interface UnbanUserRequest {
    reason: string;
}

export interface AdminUserPreview {
    user: AdminUserDetail;
    organizations: Array<{
        id: string;
        name: string;
        slug?: string | null;
        owner_user_id: string;
        created_at: string;
        updated_at: string;
    }>;
    subscriptions: Array<{
        id: string;
        organization_id: string;
        plan_id: string;
        status: string;
        is_enterprise: boolean;
        current_period_end?: string | null;
        trial_end?: string | null;
    }>;
    email_accounts: Array<{
        id: string;
        email: string;
        organization_id?: string | null;
        status: string;
        provider: string;
        warmup_enabled: boolean;
        last_synced_at: string;
    }>;
    recent_bans: UserBan[];
    rate_limits?: AdminUserRateLimits | null;
}

// /admin/organizations* — workspace admin (read-only slice).

export interface AdminOrgListItem {
    id: string;
    name: string;
    slug?: string | null;
    owner_user_id: string;
    owner_email: string;
    owner_first_name: string;
    owner_last_name: string;
    owner_banned_at?: string | null;
    created_at: string;
    deletion_scheduled_for?: string | null;
    member_count: number;
    email_account_count: number;
    campaign_count: number;
    active_campaigns: number;
    plan_name?: string | null;
    plan_public?: boolean | null;
    is_enterprise: boolean;
}

export interface OrganizationLimits {
    max_campaigns?: number | null;
    max_active_campaigns?: number | null;
    max_team_members?: number | null;
    max_email_accounts?: number | null;
    max_contacts?: number | null;
    daily_campaign_limit?: number | null;
}

export interface OrganizationCounts {
    total_campaigns: number;
    active_campaigns: number;
    total_contacts: number;
    total_members: number;
    email_accounts: number;
    emails_sent_today: number;
}

export interface OrganizationLimitOverrides {
    organization_id: string;
    max_campaigns: number;
    max_active_campaigns: number;
    max_team_members: number;
    max_email_accounts: number;
    max_contacts: number;
    daily_campaign_limit: number;
    granted_by?: string | null;
    granted_at: string;
    updated_at: string;
    notes: string;
}

export interface UpdateOrgOverridesRequest {
    max_campaigns?: number;
    max_active_campaigns?: number;
    max_team_members?: number;
    max_email_accounts?: number;
    max_contacts?: number;
    daily_campaign_limit?: number;
    notes?: string;
}

export interface AdminOrgDetail extends AdminOrgListItem {
    updated_at: string;
    deletion_scheduled_at?: string | null;
    limits?: OrganizationLimits | null;
    overrides?: OrganizationLimitOverrides | null;
    effective_limits?: OrganizationLimits | null;
    counts?: OrganizationCounts | null;
    plan_name?: string | null;
    subscription_status?: string | null;
    is_enterprise: boolean;
    current_period_end?: string | null;
    trial_end?: string | null;
}

export interface AdminOrgsResult {
    data: AdminOrgListItem[];
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface AdminOrgMember {
    id: string;
    organization_id: string;
    user_id: string;
    role: string;
    permissions: number;
    invited_by?: string | null;
    invited_at: string;
    accepted_at?: string | null;
    user?: {
        id: string;
        first_name: string;
        last_name: string;
        email: string;
    };
}

export interface AdminOrgMembersResult {
    data: AdminOrgMember[];
}

export interface AdminOrgSearch {
    q?: string;
    status?: "active" | "pending_deletion" | "";
    plan_id?: string;
    plan_visibility?: "public" | "private" | "none" | "";
    created_within?: number; // days; omit for any
    has_overrides?: boolean;
    enterprise?: boolean;
    // Subscription state
    subscription_status?: string;
    cancel_at_period_end?: boolean;
    has_active_subscription?: boolean;
    no_subscription?: boolean;
    owner_banned?: boolean;
    // Relationship existence
    has_active_campaigns?: boolean;
    has_email_accounts?: boolean;
    // Count ranges
    member_count_min?: number;
    member_count_max?: number;
    email_account_count_min?: number;
    email_account_count_max?: number;
    campaign_count_min?: number;
    campaign_count_max?: number;
    // Date ranges (YYYY-MM-DD)
    created_after?: string;
    created_before?: string;
    trial_end_after?: string;
    trial_end_before?: string;
    current_period_end_after?: string;
    current_period_end_before?: string;
    updated_after?: string;
    updated_before?: string;
    cursor?: string;
    limit?: number;
    sort_by?: "created_at" | "name" | "owner_email" | "member_count" | "email_account_count" | "campaign_count";
    sort_desc?: boolean;
}
