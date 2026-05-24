import Request from "../../Request";

export default async function deleteContact(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/contacts/${id}`,
        authorization: true,
    })
}
