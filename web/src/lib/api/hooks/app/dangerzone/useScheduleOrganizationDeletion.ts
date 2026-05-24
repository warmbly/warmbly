import { useMutation, useQueryClient } from "@tanstack/react-query";
import scheduleOrganizationDeletion, {
    type ScheduleDeletionPayload,
} from "@/lib/api/client/app/dangerzone/scheduleOrganizationDeletion";

export default function useScheduleOrganizationDeletion() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: ScheduleDeletionPayload) => scheduleOrganizationDeletion(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["dangerzone", "organization"] });
            queryClient.invalidateQueries({ queryKey: ["organizations", "current"] });
            queryClient.invalidateQueries({ queryKey: ["organizations"] });
        },
    });
}
