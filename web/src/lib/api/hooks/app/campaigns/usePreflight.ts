import { useQuery } from "@tanstack/react-query";
import runPreflight from "@/lib/api/client/app/campaigns/runPreflight";

// Runs when the launch dialog opens. Kept fresh-on-open rather than cached, so
// a user who fixes a step and reopens sees the fix.
export default function usePreflight(campaignId: string, enabled: boolean) {
    return useQuery({
        queryKey: ["app", "campaigns", campaignId, "preflight"],
        queryFn: () => runPreflight(campaignId),
        enabled: enabled && !!campaignId,
        staleTime: 0,
        gcTime: 0,
        retry: false,
    });
}
