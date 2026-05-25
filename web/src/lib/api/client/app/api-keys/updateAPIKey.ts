import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export interface UpdateAPIKeyInput {
    name?: string;
    description?: string;
    permissions?: number;
    allowed_ips?: string[];
    allowed_email_accounts?: string[];
    rate_limit_per_minute?: number;
}

export default async function updateAPIKey(id: string, data: UpdateAPIKeyInput): Promise<APIKey> {
    return await Request<APIKey>({
        method: "PATCH",
        url: `/api-keys/${id}`,
        data,
        authorization: true,
    });
}
