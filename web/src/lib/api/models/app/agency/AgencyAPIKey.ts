import type Pagination from "@/lib/api/models/app/Pagination";

export interface AgencyAPIKey {
    id: string;
    agency_id: string;
    created_by: string;
    name: string;
    key_prefix: string;
    key_suffix: string;
    permissions: number;
    allowed_ips: string[];
    rate_limit_per_minute: number;
    status: "active" | "revoked";
    last_used_at?: string;
    last_request_ip?: string;
    expires_at?: string;
    revoked_at?: string;
    created_at: string;
}

export interface AgencyAPIKeyWithSecret extends AgencyAPIKey {
    secret: string;
}

export interface AgencyAPIKeysPage {
    data: AgencyAPIKey[];
    pagination: Pagination;
}

export interface AgencyAPIPermission {
    name: string;
    value: number;
    description: string;
    category: string;
}

export interface AgencyAPIKeyPermissions {
    workspace: AgencyAPIPermission[];
    agency: AgencyAPIPermission[];
    presets: { read_only: number; full_access: number };
    grantable: number;
}

export interface CreateAgencyAPIKey {
    name: string;
    permissions: number;
    allowed_ips?: string[];
}

export interface UpdateAgencyAPIKey {
    name?: string;
    allowed_ips?: string[];
}

/** An expired key still reads "active" in the column. */
export function agencyKeyLive(k: AgencyAPIKey): boolean {
    return k.status === "active" && (!k.expires_at || new Date(k.expires_at).getTime() > Date.now());
}
