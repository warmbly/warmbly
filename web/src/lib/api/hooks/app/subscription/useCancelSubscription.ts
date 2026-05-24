import { useMutation, useQueryClient } from "@tanstack/react-query";
import cancelSubscription from "@/lib/api/client/app/subscription/cancelSubscription";

export default function useCancelSubscription() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => cancelSubscription(),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["subscription"],
            })
        }
    })
}
