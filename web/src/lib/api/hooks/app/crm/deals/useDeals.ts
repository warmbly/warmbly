import { useQuery } from "@tanstack/react-query";
import listDeals, { type ListDealsParams } from "@/lib/api/client/app/crm/deals/listDeals";

export default function useDeals(params: ListDealsParams = {}) {
    return useQuery({
        queryKey: ["crm", "deals", "list", params],
        queryFn: () => listDeals(params),
        staleTime: 30_000,
    });
}
