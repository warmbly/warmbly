// API key shape as returned by the backend.
//
// `permissions` is a bitmask (uint64) past bit 31, so test and combine it with
// @/lib/bitmask, never `&`/`|`/`~` (32-bit in JavaScript). A number holds it
// exactly up to bit 53. Resolve names <-> bits using APIPermission.value
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
    last_used_at?: Date | null;
    last_request_ip?: string | null;
    expires_at?: Date | null;
    revoked_at?: Date | null;
    revoked_reason?: string | null;

    created_at: Date;
    updated_at: Date;
}

// The `status` column only ever holds "active" or "revoked": expiry is applied
// when the backend reads a key, so one past its `expires_at` still reports
// "active" while authenticating nothing. Everything user-facing goes through
// these two so the dashboard says the same thing the API does.
export function keyCanAuthenticate(key: APIKey): boolean {
    if (key.status !== "active") return false;
    return !key.expires_at || new Date(key.expires_at).getTime() > Date.now();
}

export function keyStatus(key: APIKey): APIKeyStatus {
    if (key.status === "active" && !keyCanAuthenticate(key)) return "expired";
    return key.status;
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
