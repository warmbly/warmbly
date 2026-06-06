import { useMutation, useQueryClient } from "@tanstack/react-query";
import verifyCampaignTrackingDomain from "@/lib/api/client/app/campaigns/verifyCampaignTrackingDomain";

export default function useVerifyCampaignTrackingDomain(campaignId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: () => verifyCampaignTrackingDomain(campaignId),
        onSuccess: () => {
            // The campaign row carries the verified flags; refetch the detail.
            queryClient.invalidateQueries({ queryKey: ["campaigns", campaignId] });
        },
    });
}
