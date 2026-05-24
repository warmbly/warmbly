import type DangerZoneStatus from "@/lib/api/models/app/dangerzone/DangerZoneStatus";
import Request from "../../Request";

export default async function getAccountDangerZone(): Promise<DangerZoneStatus> {
    return await Request<DangerZoneStatus>({
        method: "GET",
        url: `/me/danger-zone`,
        authorization: true,
    });
}
