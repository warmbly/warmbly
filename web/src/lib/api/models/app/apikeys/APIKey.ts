// API key shape as returned by the backend.
//
// `permissions` is a bitmask (uint64). JavaScript safely handles the
// number range we use (currently 19 bits, ceiling at 53 bits without
// switching to BigInt). Resolve names <-> bits using APIPermission.value
// from /api-keys/permissions.

export type APIKeyStatus = "active" | "revoked" | "expired";

export default interface APIKey {
    id: string;
    user_id: string;
    organization_id: string;
    name: string;
    description?: string | null;
    key_prefix: string;
    key_suffix: string;
    permissions: number;

    allowed_ips?: string[];
    allowed_email_accounts?: string[];

    rate_limit_per_minute: number;

    status: APIKeyStatus;
    last_used_at?: string | null;
    last_request_ip?: string | null;
    expires_at?: string | null;
    revoked_at?: string | null;
    revoked_reason?: string | null;

    created_at: string;
    updated_at: string;
}

export interface APIKeyWithSecret extends APIKey {
    secret: string;
}

export interface APIKeysResult {
    data: APIKey[];
    pagination: {
        next_cursor?: string | null;
        has_more: boolean;
    };
}
