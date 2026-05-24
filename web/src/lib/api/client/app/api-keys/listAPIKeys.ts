import type { APIKeysResult } from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export default async function listAPIKeys(params?: { cursor?: string; limit?: number }): Promise<APIKeysResult> {
    const qs = new URLSearchParams();
    if (params?.cursor) qs.set("cursor", params.cursor);
    if (params?.limit) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    return await Request<APIKeysResult>({
        method: "GET",
        url: `/api-keys${suffix}`,
        authorization: true,
    });
}
