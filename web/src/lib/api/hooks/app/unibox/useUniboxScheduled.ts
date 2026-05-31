import { useQuery } from "@tanstack/react-query";
import listScheduled from "@/lib/api/client/app/unibox/listScheduled";

// Pending outbound sends for the user's mailboxes. Polled lightly
// because the count moves naturally as scheduled times pass — we
// don't have a realtime "task fired" event yet.
export default function useUniboxScheduled(enabled = true) {
    return useQuery({
        queryKey: ["unibox", "scheduled"],
        queryFn: () => listScheduled(),
        enabled,
        staleTime: 15_000,
        refetchInterval: 30_000,
        refetchOnWindowFocus: true,
    });
}
