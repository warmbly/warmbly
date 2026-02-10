import type WarmupAnalytics from "@/lib/api/models/app/analytics/WarmupAnalytics";
import Request from "../../Request";

export default async function getWarmup(): Promise<WarmupAnalytics> {
    return await Request<WarmupAnalytics>({
        method: "GET",
        url: `/analytics/warmup`,
        authorization: true,
    })
}
