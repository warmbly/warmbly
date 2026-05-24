import Request from "../../Request";

export default async function acceptInvitation(data: { invitation_id: string }): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/invitations/accept`,
        data,
        authorization: true,
    })
}
