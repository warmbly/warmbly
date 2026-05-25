import { useQuery } from "@tanstack/react-query";
import getAPIKeyAnalytics, { type AnalyticsParams } from "@/lib/api/client/app/api-keys/getAPIKeyAnalytics";

export default function useAPIKeyAnalytics(keyID: string | "all", params?: AnalyticsParams) {
    return useQuery({
        queryKey: ["api-keys", "analytics", keyID, params],
        queryFn: () => getAPIKeyAnalytics(keyID, params),
        refetchInterval: 60_000,
    });
}
