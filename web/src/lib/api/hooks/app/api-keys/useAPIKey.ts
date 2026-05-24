import { useQuery } from "@tanstack/react-query";
import getAPIKey from "@/lib/api/client/app/api-keys/getAPIKey";

export default function useAPIKey(id: string) {
    return useQuery({
        queryKey: ["api-keys", id],
        queryFn: () => getAPIKey(id),
        enabled: !!id,
    })
}
