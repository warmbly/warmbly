import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateSendingBehavior from "@/lib/api/client/app/emails/updateSendingBehavior";
import type SendingBehavior from "@/lib/api/models/app/emails/SendingBehavior";
import type { SendingBehaviorPatch } from "@/lib/api/models/app/emails/SendingBehavior";

export default function useUpdateSendingBehavior(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (patch: SendingBehaviorPatch) => updateSendingBehavior(id, patch),
        onSuccess: (data) => {
            queryClient.setQueryData<SendingBehavior>(["emails", id, "behavior"], data);
            // Today's plan keeps whatever it already rolled, but the ranges it
            // will roll from tomorrow have changed, so drop the cached view.
            queryClient.invalidateQueries({ queryKey: ["emails", id, "behavior", "plan"] });
        },
    });
}
