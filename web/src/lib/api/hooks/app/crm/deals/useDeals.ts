import { useQuery } from "@tanstack/react-query";
import listDeals from "@/lib/api/client/app/crm/deals/listDeals";

export default function useDeals() {
    return useQuery({
        queryKey: ["crm", "deals", "list"],
        queryFn: () => listDeals(),
    })
}
