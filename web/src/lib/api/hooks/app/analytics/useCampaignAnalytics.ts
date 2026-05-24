import { useQuery } from "@tanstack/react-query";
import getCampaignAnalytics from "@/lib/api/client/app/analytics/getCampaignAnalytics";

export default function useCampaignAnalytics(id: string) {
    return useQuery({
        queryKey: ["analytics", "campaigns", id],
        queryFn: () => getCampaignAnalytics(id),
        enabled: !!id,
    })
}
