import type DangerZoneStatus from "@/lib/api/models/app/dangerzone/DangerZoneStatus";
import Request from "../../Request";

export default async function getOrganizationDangerZone(): Promise<DangerZoneStatus> {
    return await Request<DangerZoneStatus>({
        method: "GET",
        url: `/organization/current/danger-zone`,
        authorization: true,
    });
}
