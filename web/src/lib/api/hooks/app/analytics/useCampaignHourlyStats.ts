import { useQuery } from "@tanstack/react-query";
import getCampaignHourlyStats from "@/lib/api/client/app/analytics/getCampaignHourlyStats";

export default function useCampaignHourlyStats(id: string) {
    return useQuery({
        queryKey: ["analytics", "campaigns", id, "hourly"],
        queryFn: () => getCampaignHourlyStats(id),
        enabled: !!id,
    })
}
