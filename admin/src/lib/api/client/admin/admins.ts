// /admin/admins/* and /admin/permissions — who holds operator-panel bits.
// Shapes mirror AdminInfo / AdminsResult in internal/models/admin.go and
// PermissionInfo in internal/models/admin_permission.go.

import { Request } from "@/lib/api/client";
import type { AdminUserSummary } from "@/lib/api/models/admin";

export interface AdminInfo {
    id: string;
    first_name: string;
    last_name: string;
    email: string;
    /** Bitmask, not a list: models.AdminPermission serializes as a uint32. */
    admin_permissions: number;
    admin_granted_at?: string | null;
    admin_granted_by?: string | null;
    granted_by_user?: AdminUserSummary | null;
}

export interface AdminsResult {
    data: AdminInfo[] | null;
    pagination: {
        total?: number | null;
        next_cursor?: string | null;
        has_more: boolean;
    };
}

export interface PermissionInfo {
    name: string;
    /** The single bit this permission occupies. */
    permission: number;
    description: string;
    category: string;
}

export function listAdmins(cursor?: string, limit = 100): Promise<AdminsResult> {
    const usp = new URLSearchParams();
    if (cursor) usp.set("cursor", cursor);
    usp.set("limit", String(limit));
    return Request({
        method: "GET",
        url: `/admin/admins?${usp.toString()}`,
        authorization: true,
    });
}

// The backend wraps the catalog as { permissions: [...] }; unwrap it here.
export function listAdminPermissions(): Promise<PermissionInfo[]> {
    return Request<{ permissions: PermissionInfo[] | null }>({
        method: "GET",
        url: "/admin/permissions",
        authorization: true,
    }).then((r) => r.permissions ?? []);
}

// GrantAdminRequest takes only the bitmask; presets are resolved client-side.
export function grantAdminPermissions(
    userId: string,
    permissions: number,
): Promise<{ message: string }> {
    return Request({
        method: "POST",
        url: `/admin/admins/${userId}/grant`,
        authorization: true,
        data: { permissions },
    });
}

export function revokeAdminPermissions(userId: string): Promise<{ message: string }> {
    return Request({
        method: "POST",
        url: `/admin/admins/${userId}/revoke`,
        authorization: true,
    });
}
