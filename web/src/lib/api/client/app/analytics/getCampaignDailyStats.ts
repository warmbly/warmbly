import type DailyStats from "@/lib/api/models/app/analytics/DailyStats";
import Request from "../../Request";

export default async function getCampaignDailyStats(id: string): Promise<DailyStats[]> {
    return await Request<DailyStats[]>({
        method: "GET",
        url: `/analytics/campaigns/${id}/daily`,
        authorization: true,
    })
}
