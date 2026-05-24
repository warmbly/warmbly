import { useQuery } from "@tanstack/react-query";
import getAccountStatuses from "@/lib/api/client/app/analytics/getAccountStatuses";

export default function useAccountStatuses() {
    return useQuery({
        queryKey: ["analytics", "accounts", "list"],
        queryFn: () => getAccountStatuses(),
    })
}
