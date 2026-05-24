import Request from "../../Request";

export default async function removeEmail(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/emails/${id}`,
        authorization: true,
    })
}
