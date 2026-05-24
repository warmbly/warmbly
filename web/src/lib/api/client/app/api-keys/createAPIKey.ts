import type { APIKeyWithSecret } from "@/lib/api/models/app/apikeys/APIKey";
import Request from "../../Request";

export interface CreateAPIKeyInput {
    name: string;
    description?: string;
    permissions: number;
    allowed_ips?: string[];
    allowed_email_accounts?: string[];
    rate_limit_per_minute?: number;
    expires_at?: string;
}

export default async function createAPIKey(data: CreateAPIKeyInput): Promise<APIKeyWithSecret> {
    return await Request<APIKeyWithSecret>({
        method: "POST",
        url: `/api-keys`,
        data,
        authorization: true,
    });
}
