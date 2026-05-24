import type { APIKeyAnalytics } from "@/lib/api/models/app/apikeys/APIKeyAnalytics";
import Request from "../../Request";

export interface AnalyticsParams {
    from?: string;
    to?: string;
    interval?: "minute" | "hour" | "day";
}

export default async function getAPIKeyAnalytics(keyID: string | "all", params?: AnalyticsParams): Promise<APIKeyAnalytics> {
    const qs = new URLSearchParams();
    if (params?.from) qs.set("from", params.from);
    if (params?.to) qs.set("to", params.to);
    if (params?.interval) qs.set("interval", params.interval);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const path = keyID === "all" ? `/api-keys/usage/analytics${suffix}` : `/api-keys/${keyID}/analytics${suffix}`;

    return await Request<APIKeyAnalytics>({
        method: "GET",
        url: path,
        authorization: true,
    });
}
