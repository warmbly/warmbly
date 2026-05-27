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
