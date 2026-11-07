import { useQuery } from "@tanstack/react-query";
import getAccountStatus from "@/lib/api/client/app/analytics/getAccountStatus";

export default function useAccountStatus(id: string) {
    return useQuery({
        queryKey: ["analytics", "accounts", id],
        queryFn: () => getAccountStatus(id),
        enabled: !!id,
        // Routine recovery persists without a realtime event.
        refetchInterval: 60_000,
        refetchOnWindowFocus: true,
    });
}
