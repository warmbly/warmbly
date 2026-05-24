import { useMutation, useQueryClient } from "@tanstack/react-query";
import cancelAccountDeletion from "@/lib/api/client/app/dangerzone/cancelAccountDeletion";
import type { CancelDeletionPayload } from "@/lib/api/client/app/dangerzone/cancelOrganizationDeletion";

export default function useCancelAccountDeletion() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: CancelDeletionPayload | undefined) =>
            cancelAccountDeletion(data ?? {}),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["dangerzone", "account"] });
            queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
        },
    });
}
