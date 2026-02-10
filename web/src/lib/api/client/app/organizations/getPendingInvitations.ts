import type Invitation from "@/lib/api/models/app/organizations/Invitation";
import Request from "../../Request";

export default async function getPendingInvitations(): Promise<Invitation[]> {
    return await Request<Invitation[]>({
        method: "GET",
        url: `/organization/invitations`,
        authorization: true,
    })
}
