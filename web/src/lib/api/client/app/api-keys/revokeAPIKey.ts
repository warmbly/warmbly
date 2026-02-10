import Request from "../../Request";

export default async function revokeAPIKey(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/api-keys/${id}`,
        authorization: true,
    })
}
