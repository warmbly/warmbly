import Request from "../../../Request";

export default async function deleteRole(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/roles/${id}`,
        authorization: true,
    })
}
