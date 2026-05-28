// /admin/users/* — platform user admin. Search, profile, ban/unban,
// rate-limit overrides. All endpoints already exist on the backend.

import { Request } from "@/lib/api/client";
import type {
    AdminUserDetail,
    AdminUserPreview,
    AdminUserRateLimits,
    AdminUserSearchParams,
    AdminUsersResult,
    BanUserRequest,
    UnbanUserRequest,
    UpdateUserRateLimitsRequest,
    UserBan,
} from "@/lib/api/models/admin";

function toQuery(params: AdminUserSearchParams): string {
    const usp = new URLSearchParams();
    if (params.q) usp.set("q", params.q);
    if (params.status) usp.set("status", params.status);
    if (params.is_admin != null) usp.set("is_admin", String(params.is_admin));
    if (params.cursor) usp.set("cursor", params.cursor);
    if (params.limit != null) usp.set("limit", String(params.limit));
    if (params.sort_by) usp.set("sort_by", params.sort_by);
    if (params.sort_desc) usp.set("sort_desc", "true");
    const s = usp.toString();
    return s ? `?${s}` : "";
}

export function searchUsers(
    params: AdminUserSearchParams = {},
): Promise<AdminUsersResult> {
    return Request({
        method: "GET",
        url: `/admin/users${toQuery(params)}`,
        authorization: true,
    });
}

export function getUser(id: string): Promise<AdminUserDetail> {
    return Request({
        method: "GET",
        url: `/admin/users/${id}`,
        authorization: true,
    });
}

export function getUserPreview(id: string): Promise<AdminUserPreview> {
    return Request({
        method: "GET",
        url: `/admin/users/${id}/preview`,
        authorization: true,
    });
}

export function getUserBans(id: string): Promise<{ data: UserBan[] }> {
    return Request({
        method: "GET",
        url: `/admin/users/${id}/bans`,
        authorization: true,
    });
}

export function banUser(id: string, body: BanUserRequest): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/users/${id}/ban`,
        authorization: true,
        data: body,
    });
}

export function unbanUser(id: string, body: UnbanUserRequest): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/users/${id}/unban`,
        authorization: true,
        data: body,
    });
}

export function getUserRateLimits(id: string): Promise<AdminUserRateLimits | null> {
    return Request({
        method: "GET",
        url: `/admin/users/${id}/rate-limits`,
        authorization: true,
    });
}

export function updateUserRateLimits(
    id: string,
    body: UpdateUserRateLimitsRequest,
): Promise<AdminUserRateLimits> {
    return Request({
        method: "PATCH",
        url: `/admin/users/${id}/rate-limits`,
        authorization: true,
        data: body,
    });
}
