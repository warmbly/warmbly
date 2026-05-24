import type Organization from "@/lib/api/models/app/organizations/Organization";
import Request from "../../Request";

export default async function getCurrentOrganization(): Promise<Organization> {
    return await Request<Organization>({
        method: "GET",
        url: `/organization/current`,
        authorization: true,
    })
}
