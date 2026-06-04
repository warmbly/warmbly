import { useMutation, useQueryClient } from "@tanstack/react-query";
import createSource from "@/lib/api/client/app/leadsync/createSource";

// Saves a new sync source and refreshes every source list.
export default function useCreateLeadSyncSource() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: createSource,
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["lead-sync", "sources"] });
        },
    });
}
