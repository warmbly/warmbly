import type CampaignAnalytics from "@/lib/api/models/app/analytics/CampaignAnalytics";
import Request from "../../Request";

export default async function getCampaignAnalytics(id: string): Promise<CampaignAnalytics> {
    return await Request<CampaignAnalytics>({
        method: "GET",
        url: `/analytics/campaigns/${id}`,
        authorization: true,
    })
}
