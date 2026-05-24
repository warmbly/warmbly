import Request from "../../../Request";

export default async function deleteSequence(campaign_id: string, sequence_id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/campaigns/${campaign_id}/sequences/${sequence_id}`,
        authorization: true,
    })
}
