import type Invitation from "@/lib/api/models/app/organizations/Invitation";
import Request from "../../Request";

export default async function getMyInvitations(): Promise<Invitation[]> {
    return await Request<Invitation[]>({
        method: "GET",
        url: `/invitations`,
        authorization: true,
    })
}
