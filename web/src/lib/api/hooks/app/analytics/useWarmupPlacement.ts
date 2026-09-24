import { keepPreviousData, useQuery } from "@tanstack/react-query";
import getWarmupPlacement from "@/lib/api/client/app/analytics/getWarmupPlacement";

// Keyed under ["analytics", "warmup", "placement"], which WARMUP_PLACEMENT
// realtime events refresh.
export default function useWarmupPlacement(emailId: string | undefined, from: string, to: string) {
    return useQuery({
        queryKey: ["analytics", "warmup", "placement", emailId ?? "all", from, to],
        queryFn: () => getWarmupPlacement(emailId, from, to),
        placeholderData: keepPreviousData,
    });
}
