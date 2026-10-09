import type DashboardOverview from "@/lib/api/models/app/analytics/DashboardOverview";
import type { DashboardCampaignFilter } from "@/lib/api/models/app/analytics/DashboardOverview";
import Request from "../../Request";

// Workspace dashboard analytics. Returned as a bare object (no {data} envelope),
// so no unwrap is needed here — but the period must be forwarded.
export default async function getDashboard(period: string = "7d", filter?: DashboardCampaignFilter): Promise<DashboardOverview> {
    const params = new URLSearchParams({ period });
    if (filter?.campaigns.length) params.set("campaign_ids", filter.campaigns.join(","));
    if (filter?.folders.length) params.set("folder_ids", filter.folders.join(","));
    return await Request<DashboardOverview>({
        method: "GET",
        url: `/analytics/dashboard?${params.toString()}`,
        authorization: true,
    })
}
