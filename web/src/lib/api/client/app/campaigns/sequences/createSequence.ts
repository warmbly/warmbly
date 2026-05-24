import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import Request from "../../../Request";

export default async function createSequence(campaign_id: string): Promise<Sequence> {
    return await Request<Sequence>({
        method: "POST",
        url: `/campaigns/${campaign_id}/sequences`,
        authorization: true,
    })
}
