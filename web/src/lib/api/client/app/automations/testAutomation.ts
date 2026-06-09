import type { DryRunResponse } from "@/lib/api/models/app/automations/Automation";
import Request from "../../Request";

// testAutomation runs an automation against sample (or provided) data without
// side effects, returning the path taken + per-action previews.
export default async function testAutomation(
    id: string,
    data?: Record<string, unknown>,
): Promise<DryRunResponse> {
    return await Request<DryRunResponse>({
        method: "POST",
        url: `/automations/${id}/test`,
        authorization: true,
        data: data ? { data } : {},
    });
}
