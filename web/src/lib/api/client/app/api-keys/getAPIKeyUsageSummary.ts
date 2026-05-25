import type { APIKeyUsageSummary } from "@/lib/api/models/app/apikeys/APIKeyAnalytics";
import Request from "../../Request";

export default async function getAPIKeyUsageSummary(): Promise<APIKeyUsageSummary> {
    return await Request<APIKeyUsageSummary>({
        method: "GET",
        url: `/api-keys/usage/summary`,
        authorization: true,
    });
}
