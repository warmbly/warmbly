export interface APIKeyUsageSummary {
    active_keys: number;
    revoked_keys: number;
    expired_keys: number;
    requests_24h: number;
    errors_24h: number;
    avg_latency_ms_24h: number;
    last_call_at?: string | null;
}

export interface APIKeyUsageBucket {
    bucket: string;
    total: number;
    success: number;
    client_errors: number;
    server_errors: number;
    avg_latency_ms: number;
}

export interface APIKeyEndpointStat {
    endpoint: string;
    method: string;
    count: number;
    error_count: number;
    avg_latency_ms: number;
}

export interface APIKeyAnalytics {
    api_key_id: string;
    from: string;
    to: string;
    interval: "minute" | "hour" | "day";
    buckets: APIKeyUsageBucket[];
    endpoints: APIKeyEndpointStat[];
    total: number;
    errors: number;
}

export interface APIKeyUsageLog {
    id: string;
    api_key_id: string;
    endpoint: string;
    method: string;
    ip_address: string;
    user_agent: string;
    response_code: number;
    response_time_ms: number;
    created_at: string;
}

export interface APIKeyUsageLogsResult {
    data: APIKeyUsageLog[];
    pagination: {
        next_cursor?: string | null;
        has_more: boolean;
    };
}
