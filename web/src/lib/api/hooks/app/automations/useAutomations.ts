import { useQuery } from "@tanstack/react-query";
import listAutomations from "@/lib/api/client/app/automations/listAutomations";

export function useAutomations(opts?: { enabled?: boolean }) {
    return useQuery({
        queryKey: ["automations"],
        queryFn: listAutomations,
        staleTime: 15_000,
        enabled: opts?.enabled ?? true,
    });
}
