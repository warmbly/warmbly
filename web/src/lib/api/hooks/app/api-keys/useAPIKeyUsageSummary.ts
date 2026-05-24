import { useQuery } from "@tanstack/react-query";
import getAPIKeyUsageSummary from "@/lib/api/client/app/api-keys/getAPIKeyUsageSummary";

export default function useAPIKeyUsageSummary() {
    return useQuery({
        queryKey: ["api-keys", "usage-summary"],
        queryFn: () => getAPIKeyUsageSummary(),
        refetchInterval: 30_000,
    });
}
