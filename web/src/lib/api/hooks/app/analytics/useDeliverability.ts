import { useQuery } from "@tanstack/react-query";
import getDeliverability from "@/lib/api/client/app/analytics/getDeliverability";

// Keyed under ["analytics", ...] so useRealtimeEvents' bounce/open/account
// invalidations refresh it live with no extra wiring.
export default function useDeliverability(range: "7d" | "30d" | "90d" = "7d") {
    return useQuery({
        queryKey: ["analytics", "deliverability", range],
        queryFn: () => getDeliverability(range),
    });
}
