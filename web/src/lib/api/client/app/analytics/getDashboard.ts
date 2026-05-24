import type DashboardOverview from "@/lib/api/models/app/analytics/DashboardOverview";
import Request from "../../Request";

export default async function getDashboard(): Promise<DashboardOverview> {
    return await Request<DashboardOverview>({
        method: "GET",
        url: `/analytics/dashboard`,
        authorization: true,
    })
}
