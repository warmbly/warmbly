import type Invitation from "@/lib/api/models/app/organizations/Invitation";
import Request from "../../Request";

import type MemberAccess from "@/lib/api/models/app/organizations/MemberAccess";

export default async function inviteMember(data: { email: string; role_ids?: string[]; role_id?: string; access?: MemberAccess }): Promise<Invitation> {
    return await Request<Invitation>({
        method: "POST",
        url: `/organization/members/invite`,
        data,
        authorization: true,
    })
}
