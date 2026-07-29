import { useQuery } from "@tanstack/react-query";
import getOverview from "@/lib/api/client/app/unibox/getOverview";

export default function useUniboxOverview() {
    return useQuery({
        queryKey: ["unibox", "overview"],
        queryFn: getOverview,
        staleTime: 15_000,
        // Keep the last-known counts around long enough that returning to
        // the inbox shows them instantly instead of rolling every stat up
        // from zero while the refetch is in flight.
        gcTime: 30 * 60 * 1000,
        refetchOnWindowFocus: true,
    })
}
