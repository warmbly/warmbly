import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";
import Request from "../../Request";

export default async function updateMemberRole(id: string, data: { role: string }): Promise<OrganizationMember> {
    return await Request<OrganizationMember>({
        method: "PATCH",
        url: `/organization/members/${id}`,
        data,
        authorization: true,
    })
}
