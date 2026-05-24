import { useMutation, useQueryClient } from "@tanstack/react-query";
import changePlan from "@/lib/api/client/app/subscription/changePlan";

export default function useChangePlan() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { plan_id: string }) => changePlan(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["subscription"],
            })
        }
    })
}
