import { useQuery } from "@tanstack/react-query";
import listAutomationRuns from "@/lib/api/client/app/automations/listAutomationRuns";

export function useAutomationRuns(id: string, enabled = true) {
    return useQuery({
        queryKey: ["automations", id, "runs"],
        queryFn: () => listAutomationRuns(id),
        enabled: enabled && !!id,
        staleTime: 10_000,
    });
}
