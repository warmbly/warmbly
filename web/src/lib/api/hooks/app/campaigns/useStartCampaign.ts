import { useMutation, useQueryClient } from "@tanstack/react-query";
import startCampaign from "@/lib/api/client/app/campaigns/startCampaign";

export default function useStartCampaign() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => startCampaign(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["campaigns"],
            })
        }
    })
}
