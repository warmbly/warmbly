// /admin/agencies: agencies with their plan, clients and shared send pool, and what an operator may change on them.

import { Request } from "@/lib/api/client";

export interface AdminAgency {
    id: string;
    name: string;
    owner_user_id: string;
    owner_email: string;
    plan_id?: string;
    plan?: { id: string; name?: string; monthly_send_pool?: number; agency_ai_allowance_usd?: number };
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
    // A plan is in force, from Stripe or an operator. Absent from a backend older than operator plans.
    entitled?: boolean;
    // Present only while an operator's plan is in force; `plan` is then that plan.
    granted?: { plan_id: string; until?: string };
    // The operator's plan as recorded, a lapsed one included.
    managed_plan?: AdminAgencyManagedPlan;
    // Present only when at least one value is set.
    overrides?: AdminAgencyOverrides;
}

export interface AdminAgencyManagedPlan {
    plan_id: string;
    plan_name: string;
    reason: string;
    until?: string;
    granted_at: string;
    granted_by?: string;
    active: boolean;
}

export interface AdminAgencyOverrides {
    monthly_send_pool?: number;
    ai_allowance_usd?: number;
    reason: string;
    updated_at?: string;
    updated_by?: string;
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

// Every write answers with the agency as it now stands.

export function createAgency(body: { owner_email: string; name: string }): Promise<AdminAgencyDetail> {
    return Request({ method: "POST", url: "/admin/agencies", authorization: true, data: body });
}

/** Puts the agency on an agency plan without Stripe; no `until` means no end. */
export function grantAgencyPlan(id: string, body: { plan_id: string; reason: string; until?: string }): Promise<AdminAgencyDetail> {
    return Request({ method: "PUT", url: `/admin/agencies/${id}/managed-plan`, authorization: true, data: body });
}

export function revokeAgencyPlan(id: string): Promise<AdminAgencyDetail> {
    return Request({ method: "DELETE", url: `/admin/agencies/${id}/managed-plan`, authorization: true });
}

/** Replaces both values; null is the plan's own. */
export function setAgencyOverrides(
    id: string,
    body: { monthly_send_pool: number | null; ai_allowance_usd: number | null; reason: string },
): Promise<AdminAgencyDetail> {
    return Request({ method: "PUT", url: `/admin/agencies/${id}/overrides`, authorization: true, data: body });
}

/** Adds sends to the current period only. */
export function creditAgencyPool(id: string, body: { sends: number; reason: string }): Promise<AdminAgencyDetail> {
    return Request({ method: "POST", url: `/admin/agencies/${id}/pool-credit`, authorization: true, data: body });
}

/** Adds money to the shared AI balance. Needs a recent confirmation; the key keeps a retry from adding it twice. */
export function creditAgencyAI(id: string, body: { amount_usd: number; reason: string }, idempotencyKey: string): Promise<AdminAgencyDetail> {
    return Request({
        method: "POST",
        url: `/admin/agencies/${id}/ai-credit`,
        authorization: true,
        data: body,
        headers: { "Idempotency-Key": idempotencyKey },
    });
}

/** Brings a workspace under the agency. Needs a recent confirmation. */
export function attachAgencyClient(id: string, organizationId: string): Promise<AdminAgencyDetail> {
    return Request({ method: "POST", url: `/admin/agencies/${id}/clients`, authorization: true, data: { organization_id: organizationId } });
}

export function detachAgencyClient(id: string, organizationId: string): Promise<AdminAgencyDetail> {
    return Request({ method: "DELETE", url: `/admin/agencies/${id}/clients/${organizationId}`, authorization: true });
}

/** The agency a workspace is a client of, or null. */
export function getOrganizationAgency(organizationId: string): Promise<{ agency: { id: string; name: string } | null }> {
    return Request({ method: "GET", url: `/admin/organizations/${organizationId}/agency`, authorization: true });
}
