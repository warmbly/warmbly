import Request from "../../Request";

export default async function deleteContacts(ids: string[]): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: "/contacts",
        data: ids,
        authorization: true,
    })
}
