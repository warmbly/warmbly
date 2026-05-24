import Request from "../../Request";

export default async function cancelInvitation(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/organization/invitations/${id}`,
        authorization: true,
    })
}
