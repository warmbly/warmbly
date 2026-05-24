import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import Request from "../../Request";

export default async function getCampaign(id: string): Promise<Campaign> {
    return await Request<Campaign>({
        method: "GET",
        url: `/campaigns/${id}`,
        authorization: true,
    })
}
