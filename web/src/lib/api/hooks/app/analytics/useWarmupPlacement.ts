import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import getWarmupPlacement from "@/lib/api/client/app/analytics/getWarmupPlacement";

// Keyed under ["analytics", "warmup", "placement"], which WARMUP_PLACEMENT
// realtime events refresh.
export const warmupPlacementQuery = (emailId: string | undefined, from: string, to: string) =>
    queryOptions({
        queryKey: ["analytics", "warmup", "placement", emailId ?? "all", from, to],
        queryFn: () => getWarmupPlacement(emailId, from, to),
    });

export default function useWarmupPlacement(emailId: string | undefined, from: string, to: string, enabled = true) {
    return useQuery({ ...warmupPlacementQuery(emailId, from, to), placeholderData: keepPreviousData, enabled });
}
