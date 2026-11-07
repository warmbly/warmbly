import { queryOptions, useQuery } from "@tanstack/react-query";
import getCampaignWarmupPlacement from "@/lib/api/client/app/campaigns/getCampaignWarmupPlacement";

// Receipts trickle in over hours, so a few minutes of staleness costs nothing.
export const campaignWarmupPlacementQuery = (campaignId: string) =>
    queryOptions({
        queryKey: ["campaigns", campaignId, "warmup-placement"],
        queryFn: () => getCampaignWarmupPlacement(campaignId),
        enabled: !!campaignId,
        staleTime: 5 * 60_000,
    });

export function useCampaignWarmupPlacement(campaignId: string) {
    return useQuery(campaignWarmupPlacementQuery(campaignId));
}
