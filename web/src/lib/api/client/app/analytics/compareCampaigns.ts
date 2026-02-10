import type CampaignAnalytics from "@/lib/api/models/app/analytics/CampaignAnalytics";
import Request from "../../Request";

export default async function compareCampaigns(campaignIds: string[]): Promise<CampaignAnalytics[]> {
    const params = new URLSearchParams();
    if (campaignIds.length > 0) params.append("campaign_ids", campaignIds.join(","));
    const queryString = params.toString();
    const url = `/analytics/campaigns/compare${queryString ? `?${queryString}` : ""}`;

    return await Request<CampaignAnalytics[]>({
        method: "GET",
        url,
        authorization: true,
    })
}
