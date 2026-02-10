import Request from "../../Request";

export default async function stopCampaign(id: string): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/campaigns/${id}/stop`,
        authorization: true,
    })
}
