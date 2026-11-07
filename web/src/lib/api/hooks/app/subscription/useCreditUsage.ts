import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import getCreditUsage from "@/lib/api/client/app/subscription/getCreditUsage";

// AI spend in dollars (per window vs limits, daily series, breakdowns).
export const creditUsageQuery = (days: number) =>
    queryOptions({
        queryKey: ["subscription", "credits", "usage", days],
        queryFn: () => getCreditUsage(days),
        staleTime: 60_000,
    });

// Refetched by the realtime spine on any spend; the previous range stays while a new one loads.
export default function useCreditUsage(days = 30, enabled = true) {
    return useQuery({ ...creditUsageQuery(days), placeholderData: keepPreviousData, enabled });
}
