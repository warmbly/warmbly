import { useMutation, useQueryClient } from "@tanstack/react-query";
import syncSource from "@/lib/api/client/app/leadsync/syncSource";

// "Sync now". On success, refreshes the source list (so last_synced_at /
// last_result / status update) and contacts (new/updated rows landed).
export default function useSyncLeadSyncSource() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: syncSource,
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["lead-sync", "sources"] });
            qc.invalidateQueries({ queryKey: ["contacts"] });
        },
    });
}
