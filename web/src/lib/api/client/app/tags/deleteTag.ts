import Request from "../../Request";

export default async function deleteTag(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/tags/${id}`,
        authorization: true,
    })
}
