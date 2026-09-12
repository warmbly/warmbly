// /admin/organizations/* — workspace admin (read-only). List/detail/members
// only in this slice; write paths (overrides, ban scope) land in slice 2.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";
import type {
    AdminOrgDetail,
    AdminOrgMembersResult,
    AdminOrgSearch,
    AdminOrgsResult,
    OrganizationLimitOverrides,
    OrgRisk,
    SetOrgRiskOverrideRequest,
    AdminPlan,
    ManagedPlan,
    UpdateOrgOverridesRequest,
} from "@/lib/api/models/admin";

function toQuery(params: AdminOrgSearch): string {
    return buildSearchQuery(params as Record<string, unknown>);
}

export function listOrganizations(
    params: AdminOrgSearch = {},
): Promise<AdminOrgsResult> {
    return Request({
        method: "GET",
        url: `/admin/organizations${toQuery(params)}`,
        authorization: true,
    });
}

export function getOrganization(id: string): Promise<AdminOrgDetail> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}`,
        authorization: true,
    });
}

export function getOrganizationMembers(
    id: string,
): Promise<AdminOrgMembersResult> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/members`,
        authorization: true,
    });
}

export function getOrganizationOverrides(
    id: string,
): Promise<OrganizationLimitOverrides | null> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/overrides`,
        authorization: true,
    });
}

export function updateOrganizationOverrides(
    id: string,
    body: UpdateOrgOverridesRequest,
): Promise<OrganizationLimitOverrides> {
    return Request({
        method: "PUT",
        url: `/admin/organizations/${id}/overrides`,
        authorization: true,
        data: body,
    });
}

/** Every plan, private ones included. The customer endpoint returns public
 *  plans only, and a grant is usually onto a private one. */
export function listAdminPlans(): Promise<{ plans: AdminPlan[] }> {
    return Request({
        method: "GET",
        url: `/admin/plans`,
        authorization: true,
    });
}

/** A plan an operator granted rather than Stripe. */
export function getOrganizationManagedPlan(id: string): Promise<ManagedPlan> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/managed-plan`,
        authorization: true,
    });
}

export function grantOrganizationManagedPlan(
    id: string,
    body: { plan_id: string; reason: string; until?: string | null },
): Promise<ManagedPlan> {
    return Request({
        method: "PUT",
        url: `/admin/organizations/${id}/managed-plan`,
        authorization: true,
        data: body,
    });
}

export function revokeOrganizationManagedPlan(id: string): Promise<ManagedPlan> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${id}/managed-plan`,
        authorization: true,
    });
}

/** The posture with its evidence. The customer endpoint withholds the signal
 *  blob, so this is the only place the reason a workspace was flagged is
 *  readable. */
export function getOrganizationRisk(id: string): Promise<OrgRisk> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/risk`,
        authorization: true,
    });
}

/** Pin the posture. The pin outranks the score and survives every later
 *  detector write, until it is lifted. */
export function setOrganizationRiskOverride(
    id: string,
    body: SetOrgRiskOverrideRequest,
): Promise<OrgRisk> {
    return Request({
        method: "PUT",
        url: `/admin/organizations/${id}/risk`,
        authorization: true,
        data: body,
    });
}

/** Lift the pin and hand the posture back to the evidence. */
export function clearOrganizationRiskOverride(id: string): Promise<OrgRisk> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${id}/risk`,
        authorization: true,
    });
}

/** Retract one detector's finding, which lowers the score for good. */
export function clearOrganizationRiskSignal(
    id: string,
    key: string,
): Promise<OrgRisk> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${id}/risk/signals/${encodeURIComponent(key)}`,
        authorization: true,
    });
}

// ---- API keys and webhooks (operator view) ----
// Shapes mirror AdminOrgAPIKey / AdminWebhookEndpointRow in
// internal/models/admin_ops.go.

export interface AdminOrgAPIKey {
    id: string;
    name: string;
    key_prefix: string;
    key_suffix: string;
    status: string;
    permissions: number;
    user_id: string;
    user_email: string;
    last_used_at?: string | null;
    expires_at?: string | null;
    revoked_at?: string | null;
    created_at: string;
    requests_last_7d: number;
}

export interface AdminWebhookEndpointRow {
    id: string;
    organization_id: string;
    organization_name: string;
    url: string;
    description: string;
    enabled: boolean;
    event_types: string[] | null;
    consecutive_failures: number;
    last_success_at?: string | null;
    last_failure_at?: string | null;
    last_failure_reason: string;
    deliveries_last_7d: number;
    failed_last_7d: number;
    drops_last_7d: number;
}

export function listOrganizationAPIKeys(id: string): Promise<{ data: AdminOrgAPIKey[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/api-keys`,
        authorization: true,
    });
}

// An empty reason is recorded as "revoked by platform admin" server-side.
export function revokeOrganizationAPIKey(
    id: string,
    keyId: string,
    reason?: string,
): Promise<{ id: string; organization_id: string; status: string }> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${id}/api-keys/${keyId}`,
        authorization: true,
        data: reason ? { reason } : undefined,
    });
}

export function listOrganizationWebhooks(
    id: string,
): Promise<{ data: AdminWebhookEndpointRow[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/webhooks`,
        authorization: true,
    });
}
