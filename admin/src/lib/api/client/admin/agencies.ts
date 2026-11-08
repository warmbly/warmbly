// /admin/agencies: agencies with their plan, clients and shared send pool.

import { Request } from "@/lib/api/client";

export interface AdminAgency {
    id: string;
    name: string;
    owner_user_id: string;
    owner_email: string;
    plan_id?: string;
    plan?: { id: string; name?: string; monthly_send_pool?: number };
    status: string;
    current_period_start?: string;
    current_period_end?: string;
    cancel_at_period_end: boolean;
    client_count: number;
    has_billing: boolean;
    created_at: string;
    pool_used: number;
    pool_total: number;
    unlimited: boolean;
}

export interface AdminAgencyMember {
    user_id: string;
    role: "owner" | "admin" | "member";
    email: string;
    first_name: string;
    last_name: string;
    created_at: string;
}

export interface AdminAgencyClient {
    organization_id: string;
    name: string;
    monthly_send_limit?: number | null;
    added_at: string;
}

export interface AdminAgencyUsage {
    unlimited: boolean;
    period_start: string;
    period_end: string;
    days_left: number;
    pool_size: number;
    topups: number;
    total: number;
    used: number;
    remaining: number;
    projected: number;
    clients: { organization_id: string; name: string; used: number; monthly_send_limit?: number | null }[];
}

// The shared AI balance; unmetered where the instance does not charge for AI.
export interface AdminAgencyAIBalance {
    unmetered: boolean;
    allowance_usd: number;
    allowance_left_usd: number;
    purchased_usd: number;
    spent_usd: number;
    moved_usd: number;
    balance_usd: number;
}

export interface AdminAgencyDetail {
    agency: AdminAgency;
    members: AdminAgencyMember[];
    clients: AdminAgencyClient[];
    usage: AdminAgencyUsage;
    // Absent from a backend older than the AI balance.
    ai_balance?: AdminAgencyAIBalance;
}

export function listAgencies(params: { search?: string; limit?: number; offset?: number } = {}): Promise<{ data: AdminAgency[]; total: number }> {
    const q = new URLSearchParams();
    if (params.search) q.set("search", params.search);
    if (params.limit) q.set("limit", String(params.limit));
    if (params.offset) q.set("offset", String(params.offset));
    const qs = q.toString();
    return Request({ method: "GET", url: `/admin/agencies${qs ? `?${qs}` : ""}`, authorization: true });
}

export function getAgency(id: string): Promise<AdminAgencyDetail> {
    return Request({ method: "GET", url: `/admin/agencies/${id}`, authorization: true });
}
