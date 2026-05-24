import type Organization from "@/lib/api/models/app/organizations/Organization";
import Request from "../../Request";

export default async function updateOrganization(data: Partial<Organization>): Promise<Organization> {
    return await Request<Organization>({
        method: "PATCH",
        url: `/organization/current`,
        data,
        authorization: true,
    })
}
