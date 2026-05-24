import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export default async function listAPIKeys(): Promise<APIKey[]> {
    return await Request<APIKey[]>({
        method: "GET",
        url: `/api-keys`,
        authorization: true,
    })
}
