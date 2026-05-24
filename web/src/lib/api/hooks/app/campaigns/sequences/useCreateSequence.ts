import createSequence from "@/lib/api/client/app/campaigns/sequences/createSequence";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useCreateSequence(campaign_id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => createSequence(campaign_id),
        onSuccess: (data) => {
            queryClient.setQueryData<Sequence[]>(
                ["campaigns", campaign_id, "sequences"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return [
                        ...oldData,
                        data,
                    ]
                }
            )
        }
    })
}
