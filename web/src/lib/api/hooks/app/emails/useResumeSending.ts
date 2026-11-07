import resumeSending from "@/lib/api/client/app/emails/resumeSending";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import { useMutation, useQueryClient } from "@tanstack/react-query";

// Whether the provider lets the mailbox send is the server's call, so this waits for the answer and refetches.
export default function useResumeSending(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => resumeSending(id),
        onSuccess: () => {
            queryClient.setQueryData<AccountStatus>(["analytics", "accounts", id], (prev) =>
                prev ? { ...prev, send_pause: undefined } : prev,
            );
        },
        onSettled: () => {
            void queryClient.invalidateQueries({ queryKey: ["analytics", "accounts"] });
            void queryClient.invalidateQueries({ queryKey: ["campaigns"] });
        },
    });
}
