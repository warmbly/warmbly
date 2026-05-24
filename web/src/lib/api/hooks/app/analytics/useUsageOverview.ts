import { useQuery } from "@tanstack/react-query";
import getUsageOverview from "@/lib/api/client/app/analytics/getUsageOverview";

export default function useUsageOverview() {
    return useQuery({
        queryKey: ["analytics", "usage"],
        queryFn: () => getUsageOverview(),
    })
}
