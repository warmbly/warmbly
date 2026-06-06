import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { CampaignSender, CampaignSenderInput } from "@/lib/api/models/app/campaigns/Campaign";
import replaceCampaignSenders from "@/lib/api/client/app/campaigns/replaceCampaignSenders";

export default function useReplaceCampaignSenders(campaignId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (senders: CampaignSenderInput[]) => replaceCampaignSenders(campaignId, senders),
        onSuccess: (data: CampaignSender[]) => {
            queryClient.setQueryData(["campaigns", campaignId, "senders"], data);
        },
    });
}
