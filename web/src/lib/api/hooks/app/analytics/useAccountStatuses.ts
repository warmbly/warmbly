import { useMemo, useState } from "react";
import { useQueries, type UseQueryResult } from "@tanstack/react-query";
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

// A loaded chunk keeps its ids (and so its key and cached data) while later
// list pages arrive; only new ids form new chunks. Re-packed from scratch once
// removals and additions leave it noticeably fragmented.
export function stableChunks(previous: string[][], ids: string[]): string[][] {
    const wanted = new Set(ids);
    const kept = previous.map((c) => c.filter((id) => wanted.has(id))).filter((c) => c.length > 0);
    const placed = new Set(kept.flat());
    const next = [...kept, ...chunk(ids.filter((id) => !placed.has(id)).sort())];
    return next.length > Math.ceil(ids.length / CHUNK) + 2 ? chunk([...ids].sort()) : next;
}

export default function useAccountStatuses(emailIds: string[], options?: { enabled?: boolean; sourceRevision?: number }) {
    // Sorted within a chunk so its key is stable regardless of the order the
    // mailbox list arrived in, and stable across renders so a later page of the
    // list never re-keys (and refetches) the chunks already loaded.
    const enabled = options?.enabled ?? true;
    const sourceRevision = options?.sourceRevision ?? 0;
    // The layout is state derived from the last committed one, so a render
    // React discards never shapes the next render's keys.
    const [layout, setLayout] = useState<{ ids: string[]; chunks: string[][] }>(() => ({ ids: emailIds, chunks: stableChunks([], emailIds) }));
    let chunks = layout.chunks;
    if (layout.ids !== emailIds) {
        chunks = stableChunks(layout.chunks, emailIds);
        setLayout({ ids: emailIds, chunks });
    }
    const queries = useMemo(
        () => chunks.map((ids) => ({
            queryKey: ["analytics", "accounts", "list", ids, sourceRevision],
            queryFn: ({ signal }: { signal: AbortSignal }) => getAccountStatuses(ids, signal),
            enabled: ids.length > 0 && enabled,
            // Routine recovery persists without a realtime event.
            refetchInterval: 60_000,
            refetchOnWindowFocus: true,
        })),
        [chunks, enabled, sourceRevision],
    );

    return useQueries({ queries, combine: combineAccountStatuses });
}

// TanStack reuses the aggregation only while both results and this callback are stable.
function combineAccountStatuses(results: UseQueryResult<AccountStatus[], Error>[]) {
    return {
        data: results.flatMap((r) => r.data ?? []),
        observations: results.flatMap((r) => (r.data ?? []).map((status) => ({ status, observedAt: r.dataUpdatedAt, current: r.isSuccess && !r.isFetching }))),
        isLoading: results.some((r) => r.isLoading),
        isFetching: results.some((r) => r.isFetching),
        isError: results.some((r) => r.isError),
    };
}
