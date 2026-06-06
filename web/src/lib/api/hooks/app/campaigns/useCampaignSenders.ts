import { useQuery } from "@tanstack/react-query";
import getCampaignSenders from "@/lib/api/client/app/campaigns/getCampaignSenders";

export default function useCampaignSenders(campaignId: string, enabled: boolean = true) {
    return useQuery({
        queryKey: ["campaigns", campaignId, "senders"],
        queryFn: () => getCampaignSenders(campaignId),
        enabled: enabled && !!campaignId,
    });
}
