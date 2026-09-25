// In-place edits to every loaded conversation list.
//
// A filed or snoozed conversation should leave the list the moment the user
// acts, not when the refetch lands. The row is removed from every cached
// ["unibox","search"] page here; the caller still invalidates afterwards so
// counts and ordering resync, and re-reads on error, which is the rollback.

import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { UniboxListRow } from "@/lib/api/client/app/unibox/searchIncoming";
import { markRowExit, type RowExitKind } from "@/lib/unibox/rowMotion";

interface SearchPage {
    data: UniboxListRow[];
    pagination: { has_more: boolean; next_cursor: string | null };
}

export async function removeThreadsFromLists(
    queryClient: QueryClient,
    threadIds: string[],
    /** Why they leave, which decides the direction the rows slide out. */
    exit?: RowExitKind,
): Promise<void> {
    if (threadIds.length === 0) return;
    // Measured now, while the rows are still where the user clicked them.
    if (exit) markRowExit(threadIds, exit);
    // A refetch already in flight would put the row back. Only refetches are
    // cancelled: a first load left without data has nothing queued to retry.
    await queryClient.cancelQueries({
        queryKey: ["unibox", "search"],
        predicate: (query) => query.state.data !== undefined,
    });
    const gone = new Set(threadIds);
    queryClient.setQueriesData<InfiniteData<SearchPage>>(
        { queryKey: ["unibox", "search"] },
        (old) =>
            !old
                ? old
                : {
                      ...old,
                      pages: old.pages.map((page) => ({
                          ...page,
                          data: (page.data ?? []).filter(
                              (row) => !gone.has(row.thread_id || row.id),
                          ),
                      })),
                  },
    );
}
