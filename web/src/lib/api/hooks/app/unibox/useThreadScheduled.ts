import { useQuery } from "@tanstack/react-query";
import listScheduled from "@/lib/api/client/app/unibox/listScheduled";

// Pending outbound sends queued for a single thread. ThreadView
// renders these inline so the user can see (and cancel) replies
// that haven't fired yet. Cached separately from the global
// scheduled list because they have different invalidation rhythms:
// this one moves with the open thread; the global list mirrors the
// whole pending queue.
export default function useThreadScheduled(threadId: string | undefined) {
    return useQuery({
        queryKey: ["unibox", "scheduled", "thread", threadId ?? ""],
        queryFn: () => listScheduled({ threadId }),
        enabled: !!threadId,
        staleTime: 15_000,
        refetchInterval: 30_000,
        refetchOnWindowFocus: true,
    });
}
