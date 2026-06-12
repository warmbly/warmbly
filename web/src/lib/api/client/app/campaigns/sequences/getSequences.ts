import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import Request from "../../../Request";

export default async function getSequences(campaign_id: string): Promise<Sequence[]> {
    return await Request<Sequence[]>({
        method: "GET",
        url: `/campaigns/${campaign_id}/steps`,
        authorization: true,
    })
}
