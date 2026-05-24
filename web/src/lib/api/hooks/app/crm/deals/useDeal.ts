import { useQuery } from "@tanstack/react-query";
import getDeal from "@/lib/api/client/app/crm/deals/getDeal";

export default function useDeal(id: string) {
    return useQuery({
        queryKey: ["crm", "deals", id],
        queryFn: () => getDeal(id),
        enabled: !!id,
    })
}
