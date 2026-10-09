import { queryOptions, useQuery } from "@tanstack/react-query";
import getUsageOverview from "@/lib/api/client/app/analytics/getUsageOverview";

export const usageOverviewQuery = (period: "day" | "week" | "month" = "day") =>
    queryOptions({
        queryKey: ["analytics", "usage", period],
        queryFn: () => getUsageOverview(period),
    });

export default function useUsageOverview(period: "day" | "week" | "month" = "day", enabled = true) {
    return useQuery({ ...usageOverviewQuery(period), enabled })
}
