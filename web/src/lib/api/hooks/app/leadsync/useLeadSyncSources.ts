import { useQuery } from "@tanstack/react-query";
import listSources from "@/lib/api/client/app/leadsync/listSources";

// Lists saved sync sources, optionally filtered to one campaign. Used by both
// the global Contacts > Sync sources area and the per-campaign list.
export default function useLeadSyncSources(campaignId?: string) {
    return useQuery({
        queryKey: ["lead-sync", "sources", campaignId ?? null],
        queryFn: () => listSources(campaignId),
        staleTime: 10_000,
    });
}
