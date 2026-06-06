import deleteSequence from "@/lib/api/client/app/campaigns/sequences/deleteSequence";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import { useMutation, useQueryClient } from "@tanstack/react-query";

// Delete a campaign sequence step. The id to delete is the mutate() argument
// (so one mutation instance can delete any step — important for the per-row
// useConfirm flow in StepRail). onSuccess prunes the deleted id from the cache
// using the variable passed to mutate, not a render-time-bound id.
export default function useDeleteSequence(campaign_id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (sequence_id: string) => deleteSequence(campaign_id, sequence_id),
        onSuccess: (_data, sequence_id) => {
            queryClient.setQueryData<Sequence[]>(
                ["campaigns", campaign_id, "sequences"],
                (oldData) => {
                    if (!oldData) return oldData;
                    return oldData.filter((s) => s.id !== sequence_id);
                },
            );
        },
    });
}
