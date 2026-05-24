import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export default async function getAPIKey(id: string): Promise<APIKey> {
    return await Request<APIKey>({
        method: "GET",
        url: `/api-keys/${id}`,
        authorization: true,
    })
}
