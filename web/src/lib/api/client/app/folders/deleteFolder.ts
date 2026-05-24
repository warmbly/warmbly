import Request from "../../Request";

export default async function deleteFolder(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/folders/${id}`,
        authorization: true,
    })
}
