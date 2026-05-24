import type HourlyStats from "@/lib/api/models/app/analytics/HourlyStats";
import Request from "../../Request";

export default async function getCampaignHourlyStats(id: string): Promise<HourlyStats[]> {
    return await Request<HourlyStats[]>({
        method: "GET",
        url: `/analytics/campaigns/${id}/hourly`,
        authorization: true,
    })
}
