import type { CampaignSender } from "@/lib/api/models/app/campaigns/Campaign";
import Request from "../../Request";

// GET /campaigns/:id/senders -> { data: CampaignSender[] }. Unwrap the envelope
// (Request returns the raw body) so callers always get a real array.
export default async function getCampaignSenders(campaignId: string): Promise<CampaignSender[]> {
    const res = await Request<{ data: CampaignSender[] | null } | CampaignSender[]>({
        method: "GET",
        url: `/campaigns/${campaignId}/senders`,
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
