import Request from "../../Request";

export default async function deleteCampaign(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/campaigns/${id}`,
        authorization: true,
    })
}
