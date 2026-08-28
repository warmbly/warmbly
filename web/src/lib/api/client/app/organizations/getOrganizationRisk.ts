import type { OrgRisk } from "@/lib/api/models/app/organizations/OrgRisk";
import Request from "../../Request";

export default async function getOrganizationRisk(): Promise<OrgRisk> {
    return await Request<OrgRisk>({
        method: "GET",
        url: "/organizations/current/risk",
        authorization: true,
    });
}
