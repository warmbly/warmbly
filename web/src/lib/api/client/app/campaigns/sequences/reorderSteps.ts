import Request from "../../../Request";

// Puts a straight-through sequence in a new order; every step id is sent once.
export default async function reorderSteps(campaign_id: string, stepIds: string[]): Promise<void> {
    return await Request<void>({
        method: "PATCH",
        url: `/campaigns/${campaign_id}/steps/reorder`,
        data: { step_ids: stepIds },
        authorization: true,
    })
}
