import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateSource from "@/lib/api/client/app/leadsync/updateSource";
import type { UpdateLeadSyncSource } from "@/lib/api/models/app/leadsync/LeadSync";

// Edits a saved source and refreshes every source list.
export default function useUpdateLeadSyncSource() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: { id: string; body: UpdateLeadSyncSource }) =>
            updateSource(input.id, input.body),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["lead-sync", "sources"] });
        },
    });
}
