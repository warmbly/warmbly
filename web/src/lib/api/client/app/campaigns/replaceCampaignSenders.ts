import type { CampaignSender, CampaignSenderInput } from "@/lib/api/models/app/campaigns/Campaign";
import Request from "../../Request";

// PUT /campaigns/:id/senders — full-replace of the explicit sender pool.
// Body is { senders: [...] }; response is the resolved { data: CampaignSender[] }.
export default async function replaceCampaignSenders(
    campaignId: string,
    senders: CampaignSenderInput[],
): Promise<CampaignSender[]> {
    const res = await Request<{ data: CampaignSender[] | null } | CampaignSender[]>({
        method: "PUT",
        url: `/campaigns/${campaignId}/senders`,
        data: { senders },
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
