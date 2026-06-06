import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteSource from "@/lib/api/client/app/leadsync/deleteSource";

// Deletes a saved source and refreshes every source list.
export default function useDeleteLeadSyncSource() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: deleteSource,
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["lead-sync", "sources"] });
        },
    });
}
