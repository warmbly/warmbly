import type { CampaignWarmupPlacement } from "@/lib/api/models/app/campaigns/WarmupPlacement";
import Request from "../../Request";

export default async function getCampaignWarmupPlacement(campaignId: string): Promise<CampaignWarmupPlacement> {
    const res = await Request<CampaignWarmupPlacement>({
        method: "GET",
        url: `/campaigns/${campaignId}/warmup-placement`,
        authorization: true,
    });
    return { days: res.days, data: res.data ?? [] };
}
