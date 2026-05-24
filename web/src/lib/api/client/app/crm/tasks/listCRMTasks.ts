import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import type { CRMTasksResult } from "@/lib/api/models/app/crm/CRMTask";
import Request from "../../../Request";

export interface ListCRMTasksParams {
    contact_id?: string;
    deal_id?: string;
    assigned_to?: string;
    status?: "pending" | "in_progress" | "completed" | "cancelled";
    cursor?: string;
    limit?: number;
}

export default async function listCRMTasks(params: ListCRMTasksParams = {}): Promise<CRMTasksResult> {
    const qs = new URLSearchParams();
    if (params.contact_id) qs.set("contact_id", params.contact_id);
    if (params.deal_id) qs.set("deal_id", params.deal_id);
    if (params.assigned_to) qs.set("assigned_to", params.assigned_to);
    if (params.status) qs.set("status", params.status);
    if (params.cursor) qs.set("cursor", params.cursor);
    if (params.limit) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const result = await Request<CRMTasksResult | CRMTask[]>({
        method: "GET",
        url: `/crm/tasks${suffix}`,
        authorization: true,
    });

    if (Array.isArray(result)) {
        return { data: result, pagination: { has_more: false, next_cursor: null } };
    }
    return {
        data: result.data ?? [],
        pagination: result.pagination ?? { has_more: false, next_cursor: null },
    };
}
