import { useMutation, useQueryClient } from "@tanstack/react-query";
import stopCampaign from "@/lib/api/client/app/campaigns/stopCampaign";

export default function useStopCampaign() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => stopCampaign(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["campaigns"],
            })
        }
    })
}
