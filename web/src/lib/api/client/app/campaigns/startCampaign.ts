import Request from "../../Request";

export default async function startCampaign(id: string): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/campaigns/${id}/start`,
        authorization: true,
    })
}
