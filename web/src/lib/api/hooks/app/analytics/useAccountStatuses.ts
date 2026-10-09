import { useQueries } from "@tanstack/react-query";
import getAccountStatuses from "@/lib/api/client/app/analytics/getAccountStatuses";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";

// The accounts page asks only for the mailbox ids it currently shows, in
// bounded chunks, so the status read scales with the visible page and not with
// the whole inventory. Each chunk is its own query keyed by its id set and
// stays under the ["analytics","accounts",…] prefix the realtime layer
// invalidates, so account/warmup events still refresh every row live.
//
// The chunk size stays under the backend's email_ids ceiling
// (config.AccountStatusMaxIDs = 200).
const CHUNK = 100;

function chunk(ids: string[]): string[][] {
    const out: string[][] = [];
    for (let i = 0; i < ids.length; i += CHUNK) out.push(ids.slice(i, i + CHUNK));
    return out;
}

export default function useAccountStatuses(emailIds: string[], options?: { enabled?: boolean; sourceRevision?: number }) {
    // Sort so a chunk's id set (and its query key) is stable regardless of the
    // order the mailbox list arrived in; adding a mailbox only refetches the
    // chunk it lands in.
    const chunks = chunk([...emailIds].sort());

    return useQueries({
        queries: chunks.map((ids) => ({
            queryKey: ["analytics", "accounts", "list", ids, options?.sourceRevision ?? 0],
            queryFn: ({ signal }: { signal: AbortSignal }) => getAccountStatuses(ids, signal),
            enabled: ids.length > 0 && (options?.enabled ?? true),
        })),
        // react-query memoizes this by result reference, so the merged array is
        // stable across renders until a chunk actually updates.
        combine: (results) => ({
            data: results.flatMap((r) => (r.data ?? []) as AccountStatus[]),
            observations: results.flatMap((r) => (r.data ?? []).map((status) => ({ status, observedAt: r.dataUpdatedAt }))),
            isLoading: results.some((r) => r.isLoading),
            isFetching: results.some((r) => r.isFetching),
            isError: results.some((r) => r.isError),
        }),
    });
}
