import type { DryRunResponse } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

// testAutomation runs an automation against sample (or provided) data without
// side effects, returning the path taken + per-action previews. skipNodeIds are
// action steps the caller toggled off for this run (recorded as "skipped").
export default async function testAutomation(
    id: string,
    data?: Record<string, unknown>,
    skipNodeIds?: string[],
): Promise<DryRunResponse> {
    const body: { data?: Record<string, unknown>; skip_node_ids?: string[] } = {};
    if (data) body.data = data;
    if (skipNodeIds && skipNodeIds.length > 0) body.skip_node_ids = skipNodeIds;
    return await Request<DryRunResponse>({
        method: "POST",
        url: `/automations/${id}/test`,
        authorization: true,
        data: body,
    });
}
