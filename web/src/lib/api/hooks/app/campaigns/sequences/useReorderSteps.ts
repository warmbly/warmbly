import reorderSteps from "@/lib/api/client/app/campaigns/sequences/reorderSteps";
import { useMutation, useQueryClient } from "@tanstack/react-query";

// Reorder a campaign's sequence steps. The whole ordered list of step IDs is
// passed to mutate(), so one instance can reorder any arrangement.
export default function useReorderSteps(campaign_id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (stepIds: string[]) => reorderSteps(campaign_id, stepIds),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["campaigns", campaign_id, "sequences"] });
        },
    });
}
