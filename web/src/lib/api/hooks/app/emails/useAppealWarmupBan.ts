import { useMutation, useQueryClient } from "@tanstack/react-query";
import appealWarmupBan from "@/lib/api/client/app/emails/appealWarmupBan";

// Submits a warmup-ban appeal for a mailbox, then refreshes the ban-status
// query so the banner flips to "under review" (pending_appeal) right away.
export default function useAppealWarmupBan(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (reason: string) => appealWarmupBan(id, reason),
        onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: ["analytics", "accounts", id, "warmup-ban"] });
        },
    });
}
