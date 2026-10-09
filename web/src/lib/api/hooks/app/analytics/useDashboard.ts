import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import getDashboard from "@/lib/api/client/app/analytics/getDashboard";
import type { DashboardCampaignFilter } from "@/lib/api/models/app/analytics/DashboardOverview";

// The unfiltered key stays ["analytics", "dashboard", period], shared with the sidebar meter.
export const dashboardQuery = (period: string = "7d", filter?: DashboardCampaignFilter) => {
    const scoped = !!filter && (filter.campaigns.length > 0 || filter.folders.length > 0);
    return queryOptions({
        queryKey: scoped
            ? ["analytics", "dashboard", period, { campaigns: [...filter.campaigns].sort(), folders: [...filter.folders].sort() }]
            : ["analytics", "dashboard", period],
        queryFn: () => getDashboard(period, scoped ? filter : undefined),
    });
};

export default function useDashboard(period: string = "7d", filter?: DashboardCampaignFilter) {
    // Changing the filter keeps the last numbers on screen until the new ones land.
    return useQuery({ ...dashboardQuery(period, filter), placeholderData: keepPreviousData })
}
