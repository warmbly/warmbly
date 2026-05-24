import { useQuery } from "@tanstack/react-query";
import getAccountDangerZone from "@/lib/api/client/app/dangerzone/getAccountDangerZone";

export default function useAccountDangerZone() {
    return useQuery({
        queryKey: ["dangerzone", "account"],
        queryFn: () => getAccountDangerZone(),
        staleTime: 30_000,
    });
}
