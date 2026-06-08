import { useQuery } from "@tanstack/react-query";
import getAutomation from "@/lib/api/client/app/automations/getAutomation";

export function useAutomation(id: string, enabled = true) {
    return useQuery({
        queryKey: ["automations", id],
        queryFn: () => getAutomation(id),
        enabled: enabled && !!id,
        staleTime: 15_000,
    });
}
