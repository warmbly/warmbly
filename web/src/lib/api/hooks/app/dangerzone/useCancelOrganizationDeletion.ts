import { useMutation, useQueryClient } from "@tanstack/react-query";
import cancelOrganizationDeletion, {
    type CancelDeletionPayload,
} from "@/lib/api/client/app/dangerzone/cancelOrganizationDeletion";

export default function useCancelOrganizationDeletion() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: CancelDeletionPayload | undefined) =>
            cancelOrganizationDeletion(data ?? {}),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["dangerzone", "organization"] });
            queryClient.invalidateQueries({ queryKey: ["organizations", "current"] });
            queryClient.invalidateQueries({ queryKey: ["organizations"] });
        },
    });
}
