import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import Request from "../../../Request";

export default async function updateSequence(campaign_id: string, sequence_id: string, data: Partial<Sequence>): Promise<Sequence> {
    return await Request<Sequence>({
        method: "PATCH",
        url: `/campaigns/${campaign_id}/steps/${sequence_id}`,
        data,
        authorization: true,
    })
}
