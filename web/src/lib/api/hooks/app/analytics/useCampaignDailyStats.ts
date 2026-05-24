import { useQuery } from "@tanstack/react-query";
import getCampaignDailyStats from "@/lib/api/client/app/analytics/getCampaignDailyStats";

export default function useCampaignDailyStats(id: string) {
    return useQuery({
        queryKey: ["analytics", "campaigns", id, "daily"],
        queryFn: () => getCampaignDailyStats(id),
        enabled: !!id,
    })
}
