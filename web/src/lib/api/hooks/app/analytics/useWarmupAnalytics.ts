import { useQuery } from "@tanstack/react-query";
import getWarmup from "@/lib/api/client/app/analytics/getWarmup";

export default function useWarmupAnalytics() {
    return useQuery({
        queryKey: ["analytics", "warmup"],
        queryFn: () => getWarmup(),
    })
}
