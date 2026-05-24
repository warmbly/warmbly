import type { APIKeyUsageLogsResult } from "@/lib/api/models/app/apikeys/APIKeyAnalytics";
import Request from "../../Request";

export default async function listAPIKeyUsageLogs(keyID: string, params?: { cursor?: string; limit?: number }): Promise<APIKeyUsageLogsResult> {
    const qs = new URLSearchParams();
    if (params?.cursor) qs.set("cursor", params.cursor);
    if (params?.limit) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    return await Request<APIKeyUsageLogsResult>({
        method: "GET",
        url: `/api-keys/${keyID}/logs${suffix}`,
        authorization: true,
    });
}
