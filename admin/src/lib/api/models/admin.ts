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
