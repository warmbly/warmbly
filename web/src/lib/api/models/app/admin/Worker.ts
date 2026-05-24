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

export interface CreateWorkerInput {
    name: string;
    notes?: string;
    worker_type: WorkerType;
    free_tier: boolean;
    ssh_host: string;
    ssh_port?: number;
    ssh_user?: string;
    generate_enrollment_token?: boolean;
}

export interface CreateWorkerResponse extends ManagedWorker {
    ssh_public_key: string;
    enrollment_token?: string;
    enrollment_token_ttl_seconds?: number;
}

export interface WorkerLiveStatus {
    service_active: boolean;
    container_up: boolean;
    container_image: string;
    uptime: string;
    raw: string;
}
