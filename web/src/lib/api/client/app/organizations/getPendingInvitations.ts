import type Invitation from "@/lib/api/models/app/organizations/Invitation";
import Request from "../../Request";

interface RawResponse {
    data: Invitation[] | null;
}

export default async function getPendingInvitations(): Promise<Invitation[]> {
    const res = await Request<RawResponse | Invitation[]>({
        method: "GET",
        url: `/organization/invitations`,
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
