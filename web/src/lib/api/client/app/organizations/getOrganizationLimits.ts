import type OrganizationLimits from "@/lib/api/models/app/organizations/OrganizationLimits";
import Request from "../../Request";

export default async function getOrganizationLimits(): Promise<OrganizationLimits> {
    return await Request<OrganizationLimits>({
        method: "GET",
        url: `/organization/current/limits`,
        authorization: true,
    })
}
