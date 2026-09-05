import { useMutation, useQueryClient } from "@tanstack/react-query";
import cancelSubscription, {
    type CancelSubscriptionInput,
} from "@/lib/api/client/app/subscription/cancelSubscription";

// Cancels at period end, or resumes when cancel_at_period_end is false.
export default function useCancelSubscription() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: CancelSubscriptionInput) => cancelSubscription(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["subscription"] })
        }
    })
}
