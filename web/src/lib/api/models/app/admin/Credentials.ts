export interface AWSCredentials {
    id: string;
    name: string;
    description: string;
    region: string;
    access_key_id: string;
    has_secret: boolean;
    created_at: string;
    updated_at: string;
}

export interface WorkerProfile {
    id: string;
    name: string;
    description: string;
    app_env: string;
    worker_image: string;

    kafka_bootstrap_servers: string;
    kafka_sasl_username: string;
    has_kafka_password: boolean;

    schema_registry_url: string;
    schema_registry_key: string;
    has_schema_secret: boolean;

    has_redis_url: boolean;

    aws_credential_id?: string;

    release_channel: "pinned" | "stable" | "dev";
    auto_update: boolean;
    resolved_image_tag: string;
    last_release_check_at?: string;

    created_at: string;
    updated_at: string;
}

export interface ReleasesState {
    enabled: boolean;
    github_repo: string;
    image_repo: string;
    last_checked_at?: string;
    last_error?: string;
    channels: Record<string, {
        channel: string;
        tag: string;
        image: string;
        published_at?: string;
        html_url?: string;
    }>;
}

export interface AWSCredentialsBody {
    name: string;
    description?: string;
    region: string;
    access_key_id: string;
    secret_access_key?: string; // empty on update = keep
}

export interface WorkerProfileBody {
    name: string;
    description?: string;
    app_env?: string;
    worker_image?: string;
    kafka_bootstrap_servers?: string;
    kafka_sasl_username?: string;
    kafka_sasl_password?: string;
    schema_registry_url?: string;
    schema_registry_key?: string;
    schema_registry_secret?: string;
    redis_url?: string;
    aws_credential_id?: string | null;
}

export interface ApplyResult {
    results: {
        worker_id: string;
        ok: boolean;
        error?: string;
        skipped?: string;
    }[];
}
