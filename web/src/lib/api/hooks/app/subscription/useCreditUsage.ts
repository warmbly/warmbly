import { useQuery } from "@tanstack/react-query";
import getCreditUsage from "@/lib/api/client/app/subscription/getCreditUsage";

// The AI usage overview (spend vs limits, daily series, breakdowns). Refetched
// by the realtime spine on any credit mutation.
export default function useCreditUsage(days = 30) {
    return useQuery({
        queryKey: ["subscription", "credits", "usage", days],
        queryFn: () => getCreditUsage(days),
        staleTime: 60_000,
    });
}
