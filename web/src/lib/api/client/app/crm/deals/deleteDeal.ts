import Request from "../../../Request";

export default async function deleteDeal(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/crm/deals/${id}`,
        authorization: true,
    })
}
