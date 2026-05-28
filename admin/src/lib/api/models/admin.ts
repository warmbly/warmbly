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

// Settings → Storage backends. The backend exposes a typed kind union
// (kms, blob, encrypted_keys, eventbus, cache, transport) but the
// payload shape varies per provider; we render the JSON blob raw.
export type StorageBackendKind =
    | "kms"
    | "blob"
    | "encrypted_keys"
    | "eventbus"
    | "cache"
    | "transport";

export interface StorageBackend {
    id: string;
    kind: StorageBackendKind;
    provider: string;
    label?: string;
    is_active?: boolean;
    config?: Record<string, unknown>;
    created_at?: string;
    updated_at?: string;
}

// --------------------------------------------------------------------
// Cloud providers — /admin/cloud-credentials, /admin/cloud-providers/*
// --------------------------------------------------------------------

export type CloudProvider = "hetzner";

export interface CloudCredential {
    id: string;
    provider: CloudProvider;
    name: string;
    /** Always redacted by the backend (e.g. "hcloud_***abcd"). */
    token_redacted: string;
    last_used_at?: string;
    last_test_at?: string;
    last_test_ok?: boolean;
    last_test_error?: string;
    created_at: string;
    updated_at: string;
}

export interface CloudCredentialCreate {
    provider: CloudProvider;
    name: string;
    token: string;
}

export interface CloudCredentialTestResult {
    ok: boolean;
    error?: string;
    account_email?: string;
    quota_servers?: number;
    used_servers?: number;
}

export interface HetznerLocation {
    /** e.g. "fsn1" */
    name: string;
    /** Friendly label, e.g. "Falkenstein (DE)". */
    description: string;
    country: string;
    city: string;
    network_zone?: string;
}

export interface HetznerServerType {
    /** e.g. "cx22" */
    name: string;
    description: string;
    cores: number;
    memory_gb: number;
    disk_gb: number;
    cpu_type?: string;
    /** Monthly cost in EUR (gross). */
    price_monthly_eur: number;
    /** Hourly cost in EUR (gross). */
    price_hourly_eur?: number;
    /** Extra IPv4 price per month in EUR (informational; spec says +1 free). */
    price_ipv4_monthly_eur?: number;
    architecture?: string;
}

// --------------------------------------------------------------------
// Provisioning templates — /admin/provisioning-templates
// --------------------------------------------------------------------

export type ProvisioningEgressKind = "cold_smtp" | "oauth_api" | "warmup_only";

export type ProvisioningWorkerTier = "shared_free" | "shared_premium" | "dedicated";

export interface ProvisioningLabel {
    key: string;
    value: string;
}

export interface ProvisioningConfig {
    provider: CloudProvider;
    credential_id?: string;

    // Location & server
    location: string;
    server_type: string;

    // Capacity
    server_count: number;
    ipv4_per_server: number;
    ipv6_per_server: number;

    // Worker config
    worker_tier: ProvisioningWorkerTier;
    worker_profile_id?: string;
    egress_kind: ProvisioningEgressKind;

    // Advanced
    image: string;
    datacenter?: string;
    placement_group?: string;
    private_network?: string;
    firewall: string;
    labels: ProvisioningLabel[];
}

export interface ProvisioningTemplate {
    id: string;
    name: string;
    description?: string;
    config: ProvisioningConfig;

    /** When set, this template is the default for the given tier auto-provision. */
    auto_provision_tier?: ProvisioningWorkerTier;

    is_draft: boolean;

    created_at: string;
    updated_at: string;
}

export interface ProvisioningTemplateCreate {
    name: string;
    description?: string;
    config: ProvisioningConfig;
    auto_provision_tier?: ProvisioningWorkerTier;
    is_draft: boolean;
}

// --------------------------------------------------------------------
// Worker profiles — /admin/worker-profiles (read-only here)
// --------------------------------------------------------------------

export interface WorkerProfile {
    id: string;
    name: string;
    description?: string;
}

// --------------------------------------------------------------------
// Provisioning jobs — /admin/provisioning-jobs
// --------------------------------------------------------------------

export type ProvisioningJobState =
    | "pending"
    | "creating_server"
    | "creating_ips"
    | "assigning_ips"
    | "setting_rdns"
    | "installing"
    | "verifying"
    | "completed"
    | "failed";

export interface ProvisioningJobStepProgress {
    /** "ips_created", "rdns_set", "heartbeats_received" etc. */
    key: string;
    label: string;
    done: number;
    total: number;
}

export interface ProvisioningJobTimelineEntry {
    state: ProvisioningJobState;
    at: string;
    note?: string;
}

export interface ProvisioningJob {
    id: string;
    state: ProvisioningJobState;
    /** Optional template the job was launched from. */
    template_id?: string;
    template_name?: string;
    config: ProvisioningConfig;

    progress: ProvisioningJobStepProgress[];
    timeline: ProvisioningJobTimelineEntry[];

    /** When state=completed, the worker ids created by this job. */
    created_worker_ids?: string[];

    last_error?: string;
    started_at?: string;
    completed_at?: string;

    created_at: string;
    updated_at: string;
}

export interface ProvisioningJobCreate {
    template_id?: string;
    config?: ProvisioningConfig;
    save_as_template?: {
        name: string;
        description?: string;
    };
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
    cursor?: string;
    limit?: number;
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

// /admin/enterprise/inquiries — sales pipeline for "talk to us"
// requests submitted from the marketing site.

export type EnterpriseInquiryStatus =
    | "pending"
    | "contacted"
    | "converted"
    | "declined";

export interface EnterpriseInquiry {
    id: string;
    company_name: string;
    contact_name: string;
    contact_email: string;
    estimated_volume?: number | null;
    team_size?: number | null;
    notes: string;
    status: EnterpriseInquiryStatus;
    created_at: string;
    processed_at?: string | null;
    processed_by?: string | null;
}

export interface UpdateEnterpriseInquiryRequest {
    status?: EnterpriseInquiryStatus;
    assigned_to?: string;
    notes?: string;
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
    status?: string;
    cursor?: string;
    limit?: number;
    sort_by?: string;
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
    cursor?: string;
    limit?: number;
    sort_by?: "created_at" | "name";
    sort_desc?: boolean;
}
