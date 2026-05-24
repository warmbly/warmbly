import { useMutation, useQueryClient } from "@tanstack/react-query";
import scheduleAccountDeletion from "@/lib/api/client/app/dangerzone/scheduleAccountDeletion";
import type { ScheduleDeletionPayload } from "@/lib/api/client/app/dangerzone/scheduleOrganizationDeletion";

export default function useScheduleAccountDeletion() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: ScheduleDeletionPayload) => scheduleAccountDeletion(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["dangerzone", "account"] });
            queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
        },
    });
}
