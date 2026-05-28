// /admin/organizations/* — workspace admin (read-only). List/detail/members
// only in this slice; write paths (overrides, ban scope) land in slice 2.

import { Request } from "@/lib/api/client";
import type {
    AdminOrgDetail,
    AdminOrgMembersResult,
    AdminOrgSearch,
    AdminOrgsResult,
    OrganizationLimitOverrides,
    UpdateOrgOverridesRequest,
} from "@/lib/api/models/admin";

function toQuery(params: AdminOrgSearch): string {
    const usp = new URLSearchParams();
    if (params.q) usp.set("q", params.q);
    if (params.status) usp.set("status", params.status);
    if (params.cursor) usp.set("cursor", params.cursor);
    if (params.limit != null) usp.set("limit", String(params.limit));
    if (params.sort_by) usp.set("sort_by", params.sort_by);
    if (params.sort_desc) usp.set("sort_desc", "true");
    const s = usp.toString();
    return s ? `?${s}` : "";
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
