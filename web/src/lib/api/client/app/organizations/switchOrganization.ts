import Request from "../../Request";

export default async function switchOrganization(id: string): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/organization/switch/${id}`,
        authorization: true,
    })
}
