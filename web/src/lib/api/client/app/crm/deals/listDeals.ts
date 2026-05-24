import type Deal from "@/lib/api/models/app/crm/Deal";
import type { DealsResult } from "@/lib/api/models/app/crm/Deal";
import Request from "../../../Request";

export interface ListDealsParams {
    pipeline_id?: string;
    stage_id?: string;
    status?: "open" | "won" | "lost";
    cursor?: string;
    limit?: number;
}

export default async function listDeals(params: ListDealsParams = {}): Promise<DealsResult> {
    const qs = new URLSearchParams();
    if (params.pipeline_id) qs.set("pipeline_id", params.pipeline_id);
    if (params.stage_id) qs.set("stage_id", params.stage_id);
    if (params.status) qs.set("status", params.status);
    if (params.cursor) qs.set("cursor", params.cursor);
    if (params.limit) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const result = await Request<DealsResult | Deal[]>({
        method: "GET",
        url: `/crm/deals${suffix}`,
        authorization: true,
    });

    // The handler returns the paginated shape; legacy callers may have
    // shimmed it to a bare array. Coerce either form to the canonical
    // shape so the rest of the app sees one type.
    if (Array.isArray(result)) {
        return { data: result, pagination: { has_more: false, next_cursor: null } };
    }
    return {
        data: result.data ?? [],
        pagination: result.pagination ?? { has_more: false, next_cursor: null },
    };
}
