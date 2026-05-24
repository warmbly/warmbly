import Request from "../../Request";
import type { AuditLogsResult, AuditAction, AuditEntityType } from "@/lib/api/models/app/audit/AuditLog";

export interface GetAuditLogsParams {
    cursor?: string;
    limit?: number;
    entity_type?: AuditEntityType;
    action?: AuditAction;
    date?: string;
}

export default async function getAuditLogs(
    params: GetAuditLogsParams = {},
): Promise<AuditLogsResult> {
    const q = new URLSearchParams();
    if (params.cursor) q.set("cursor", params.cursor);
    if (params.limit) q.set("limit", String(params.limit));
    if (params.entity_type) q.set("entity_type", params.entity_type);
    if (params.action) q.set("action", params.action);
    if (params.date) q.set("date", params.date);
    const suffix = q.toString() ? `?${q.toString()}` : "";

    const result = await Request<AuditLogsResult>({
        method: "GET",
        url: `/audit-logs${suffix}`,
        authorization: true,
    });

    return {
        data: result.data ?? [],
        pagination: result.pagination ?? {},
    };
}
