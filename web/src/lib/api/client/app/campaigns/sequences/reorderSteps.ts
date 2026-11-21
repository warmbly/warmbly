import Request from "../../../Request";

// Reorder the steps of a campaign sequence. The full ordered list of step IDs
// is sent so the move is atomic: the server repositions every step and
// reconnects the linear (catch-all) chain to follow the new order, preserving
// any conditional branches.
export default async function reorderSteps(campaign_id: string, stepIds: string[]): Promise<void> {
    return await Request<void>({
        method: "PATCH",
        url: `/campaigns/${campaign_id}/steps/reorder`,
        data: { stepIds },
        authorization: true,
    })
}
