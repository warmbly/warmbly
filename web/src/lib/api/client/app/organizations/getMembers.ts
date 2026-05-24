import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";
import Request from "../../Request";

interface RawResponse {
    data: OrganizationMember[] | null;
}

export default async function getMembers(): Promise<OrganizationMember[]> {
    const res = await Request<RawResponse | OrganizationMember[]>({
        method: "GET",
        url: `/organization/members`,
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
