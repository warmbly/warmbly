import { useQuery } from "@tanstack/react-query";
import listAPIKeyUsageLogs from "@/lib/api/client/app/api-keys/listAPIKeyUsageLogs";

export default function useAPIKeyUsageLogs(keyID: string | undefined, params?: { cursor?: string; limit?: number }) {
    return useQuery({
        queryKey: ["api-keys", "logs", keyID, params],
        queryFn: () => listAPIKeyUsageLogs(keyID!, params),
        enabled: !!keyID,
        refetchInterval: 15_000,
    });
}
