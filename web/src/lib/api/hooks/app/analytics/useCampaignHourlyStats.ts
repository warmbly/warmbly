import { useQuery } from "@tanstack/react-query";
import getCampaignHourlyStats from "@/lib/api/client/app/analytics/getCampaignHourlyStats";

// Defaults to today (UTC). Pass a YYYY-MM-DD date to inspect another day.
export default function useCampaignHourlyStats(id: string, date?: string) {
    const day = date ?? new Date().toISOString().slice(0, 10);
    return useQuery({
        queryKey: ["analytics", "campaigns", id, "hourly", day],
        queryFn: () => getCampaignHourlyStats(id, day),
        enabled: !!id,
    });
}
