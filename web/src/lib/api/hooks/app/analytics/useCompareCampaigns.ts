import { useQuery } from "@tanstack/react-query";
import compareCampaigns from "@/lib/api/client/app/analytics/compareCampaigns";

export default function useCompareCampaigns(campaignIds: string[]) {
    return useQuery({
        queryKey: ["analytics", "campaigns", "compare", campaignIds],
        queryFn: () => compareCampaigns(campaignIds),
        enabled: campaignIds.length > 0,
    })
}
