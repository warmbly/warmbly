import { useQuery } from "@tanstack/react-query";
import getCampaignDailyStats from "@/lib/api/client/app/analytics/getCampaignDailyStats";

function isoDay(d: Date): string {
    return d.toISOString().slice(0, 10);
}

// Defaults to the trailing `days`-day window; the backend requires from/to.
// The realtime invalidation key ["analytics","campaigns",id,"daily"] still
// matches this (prefix) so live events refresh the chart.
export default function useCampaignDailyStats(id: string, days: number = 30) {
    const to = new Date();
    const from = new Date();
    from.setUTCDate(from.getUTCDate() - (days - 1));
    const fromStr = isoDay(from);
    const toStr = isoDay(to);
    return useQuery({
        queryKey: ["analytics", "campaigns", id, "daily", fromStr, toStr],
        queryFn: () => getCampaignDailyStats(id, fromStr, toStr),
        enabled: !!id,
    });
}
