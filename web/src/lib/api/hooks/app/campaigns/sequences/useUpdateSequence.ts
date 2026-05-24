import updateSequence from "@/lib/api/client/app/campaigns/sequences/updateSequence";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useUpdateSequence(campaign_id: string, sequence_id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (sequence: Partial<Sequence>) =>
            updateSequence(campaign_id, sequence_id, sequence),
        onSuccess: (data) => {
            queryClient.setQueryData<Sequence[]>(
                ["campaigns", campaign_id, "sequences"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return oldData.map(s => s.id === sequence_id ? data : s)
                }
            )
        }
    })
}
