import { useQuery } from "@tanstack/react-query";
import getOverview from "@/lib/api/client/app/unibox/getOverview";

export default function useUniboxOverview() {
    return useQuery({
        queryKey: ["unibox", "overview"],
        queryFn: getOverview,
        staleTime: 15_000,
        refetchOnWindowFocus: true,
    })
}
