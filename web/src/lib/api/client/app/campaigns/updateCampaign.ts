import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import Request from "../../Request";

export default async function updateCampaign(id: string, campaign: Partial<Campaign>): Promise<Campaign> {
    return await Request<Campaign>({
        method: "PATCH",
        url: `/campaigns/${id}`,
        data: campaign,
        authorization: true,
    })
}
