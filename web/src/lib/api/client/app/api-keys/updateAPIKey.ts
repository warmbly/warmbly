import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export default async function updateAPIKey(id: string, data: Partial<APIKey>): Promise<APIKey> {
    return await Request<APIKey>({
        method: "PATCH",
        url: `/api-keys/${id}`,
        data,
        authorization: true,
    })
}
