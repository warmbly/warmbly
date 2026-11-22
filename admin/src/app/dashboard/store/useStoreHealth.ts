import { useQuery } from "@tanstack/react-query";
import { getStoreHealth } from "@/lib/api/client/admin/store";

// Shared by the Health tab and the tab badges, so the page reads it once.
export function useStoreHealth() {
    return useQuery({ queryKey: ["admin", "store", "health"], queryFn: getStoreHealth, staleTime: 30_000, refetchInterval: 60_000 });
}
