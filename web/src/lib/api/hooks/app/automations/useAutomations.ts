import { useQuery } from "@tanstack/react-query";
import listAutomations from "@/lib/api/client/app/automations/listAutomations";

export function useAutomations() {
    return useQuery({
        queryKey: ["automations"],
        queryFn: listAutomations,
        staleTime: 15_000,
    });
}
