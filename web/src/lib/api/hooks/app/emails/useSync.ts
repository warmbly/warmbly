import { useQuery } from "@tanstack/react-query";
import getSync from "@/lib/api/client/app/emails/getSync";

// Sync state for the mailbox drawer. Realtime ACCOUNT_SYNC_STATE events
// invalidate ["emails", id, "sync"] when the import finishes or fair use
// flips; while an import is running the worker relays progress every pass,
// so a short refetch keeps the counter moving between those events.
export default function useSync(id: string, enabled = true) {
    return useQuery({
        queryKey: ["emails", id, "sync"],
        queryFn: () => getSync(id),
        enabled: !!id && enabled,
        staleTime: 15_000,
        refetchInterval: (query) => {
            const status = query.state.data?.state?.backfill_status;
            return status === "running" || status === "pending" ? 20_000 : false;
        },
    });
}
