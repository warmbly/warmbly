import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export default async function createAPIKey(data: { name: string; permissions: string[]; expires_at?: string }): Promise<APIKey & { key: string }> {
    return await Request<APIKey & { key: string }>({
        method: "POST",
        url: `/api-keys`,
        data,
        authorization: true,
    })
}
