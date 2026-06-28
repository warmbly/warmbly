import { useMutation, useQueryClient } from "@tanstack/react-query";
import ensureReferralCode from "@/lib/api/client/app/referral/ensureReferralCode";

// Explicit "ensure"/regenerate of the referral code. GET already mints on first
// view, so this is optional; on success we refresh the summary.
export default function useEnsureReferralCode() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: () => ensureReferralCode(),
        onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: ["subscription", "referral"] });
        },
    });
}
