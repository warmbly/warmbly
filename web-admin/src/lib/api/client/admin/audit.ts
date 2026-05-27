// /admin/audit-logs

import { Request } from "@/lib/api/client";
import type {
    AdminAuditLogSearch,
    AdminAuditLogsResult,
} from "@/lib/api/models/admin";

export function searchAdminAuditLogs(
    params: AdminAuditLogSearch,
): Promise<AdminAuditLogsResult> {
    const q = new URLSearchParams();
    if (params.admin_user_id) q.set("admin_user_id", params.admin_user_id);
    if (params.action) q.set("action", params.action);
    if (params.target_type) q.set("target_type", params.target_type);
    if (params.target_id) q.set("target_id", params.target_id);
    if (params.start_date) q.set("start_date", params.start_date);
    if (params.end_date) q.set("end_date", params.end_date);
    if (params.cursor) q.set("cursor", params.cursor);
    if (params.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return Request({
        method: "GET",
        url: "/admin/audit-logs" + (qs ? "?" + qs : ""),
        authorization: true,
    });
}
