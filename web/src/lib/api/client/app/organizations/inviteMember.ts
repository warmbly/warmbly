import type Invitation from "@/lib/api/models/app/organizations/Invitation";
import Request from "../../Request";

export default async function inviteMember(data: { email: string; role: string }): Promise<Invitation> {
    return await Request<Invitation>({
        method: "POST",
        url: `/organization/members/invite`,
        data,
        authorization: true,
    })
}
