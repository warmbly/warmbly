import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import Request from "../../Request";

export default async function createCampaign(name: string, description: string): Promise<Campaign> {
    return await Request<Campaign>({
        method: "POST",
        url: `/campaigns`,
        data: {
            name,
            description,
        },
        authorization: true,
    })
}
