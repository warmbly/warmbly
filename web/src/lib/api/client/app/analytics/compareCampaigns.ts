import type CampaignComparison from "@/lib/api/models/app/analytics/CampaignComparison";
import Request from "../../Request";

// GET /analytics/campaigns/compare?ids=a,b,c&from&to. The backend reads `ids`
// (NOT campaign_ids) and REQUIRES from/to; returns a single { campaigns, period }.
export default async function compareCampaigns(
    campaignIds: string[],
    from: string,
    to: string,
): Promise<CampaignComparison> {
    const params = new URLSearchParams();
    if (campaignIds.length > 0) params.append("ids", campaignIds.join(","));
    params.append("from", from);
    params.append("to", to);

    return await Request<CampaignComparison>({
        method: "GET",
        url: `/analytics/campaigns/compare?${params.toString()}`,
        authorization: true,
    });
}
