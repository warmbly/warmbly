import type Organization from "@/lib/api/models/app/organizations/Organization";
import Request from "../../Request";

export default async function createOrganization(data: { name: string }): Promise<Organization> {
    return await Request<Organization>({
        method: "POST",
        url: `/organization`,
        data,
        authorization: true,
    })
}
