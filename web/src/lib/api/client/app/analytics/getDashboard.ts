import type DashboardOverview from "@/lib/api/models/app/analytics/DashboardOverview";
import Request from "../../Request";

// Workspace dashboard analytics. Returned as a bare object (no {data} envelope),
// so no unwrap is needed here — but the period must be forwarded.
export default async function getDashboard(period: string = "7d"): Promise<DashboardOverview> {
    return await Request<DashboardOverview>({
        method: "GET",
        url: `/analytics/dashboard?period=${encodeURIComponent(period)}`,
        authorization: true,
    })
}
