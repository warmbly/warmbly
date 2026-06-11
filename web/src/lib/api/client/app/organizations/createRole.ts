import type OrganizationRole from "@/lib/api/models/app/organizations/OrganizationRole";
import Request from "../../Request";

export default async function createRole(data: { name: string; description?: string; color?: string; permissions: number }): Promise<OrganizationRole> {
    return await Request<OrganizationRole>({
        method: "POST",
        url: `/organization/roles`,
        data,
        authorization: true,
    })
}
