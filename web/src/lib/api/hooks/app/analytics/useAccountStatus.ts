import { useQuery } from "@tanstack/react-query";
import getAccountStatus from "@/lib/api/client/app/analytics/getAccountStatus";

export default function useAccountStatus(id: string) {
    return useQuery({
        queryKey: ["analytics", "accounts", id],
        queryFn: () => getAccountStatus(id),
        enabled: !!id,
    })
}
