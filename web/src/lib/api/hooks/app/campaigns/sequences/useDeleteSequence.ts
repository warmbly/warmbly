import deleteSequence from "@/lib/api/client/app/campaigns/sequences/deleteSequence";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteSequence(campaign_id: string, sequence_id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (sequence_id: string) => deleteSequence(campaign_id, sequence_id),
        onSuccess: () => {
            queryClient.setQueryData<Sequence[]>(
                ["campaigns", campaign_id, "sequences"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return oldData.filter((s) => s.id !== sequence_id)
                }
            )
        }
    })
}
