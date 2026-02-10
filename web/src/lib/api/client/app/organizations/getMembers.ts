import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";
import Request from "../../Request";

export default async function getMembers(): Promise<OrganizationMember[]> {
    return await Request<OrganizationMember[]>({
        method: "GET",
        url: `/organization/members`,
        authorization: true,
    })
}
