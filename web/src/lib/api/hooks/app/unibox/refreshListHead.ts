// Realtime refresh of the loaded conversation lists without refetching every page.
//
// An inbox event mostly moves the top of a list, so a list holding more than one
// page refetches only its first page and splices it in; the query is then marked
// stale, so the next mount or focus reloads it whole.

import type { InfiniteData, InvalidateQueryFilters, Query, QueryClient } from "@tanstack/react-query";
import searchIncoming, { type UniboxListRow } from "@/lib/api/client/app/unibox/searchIncoming";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";
import type { Invalidate } from "@/lib/realtime/refreshCoalescer";

type Page = Awaited<ReturnType<typeof searchIncoming>>;
type SearchData = InfiniteData<Page, string | null>;

// Matches useUniboxSearch's page size, so the spliced head is the page it replaces.
const HEAD_LIMIT = 50;

const rowKey = (row: UniboxListRow) => row.thread_id || row.id;

// The server's order: newest first, then id, both descending.
function olderThan(a: UniboxListRow, b: UniboxListRow): boolean {
    const at = new Date(a.internal_date).getTime();
    const bt = new Date(b.internal_date).getTime();
    return at !== bt ? at < bt : a.id < b.id;
}

/** Replaces a list's first page with a fresh one, keeping the later pages the user scrolled to. */
export function mergeHead(old: SearchData | undefined, fresh: Page): SearchData | undefined {
    if (!old || old.pages.length === 0) return old;
    const rows = (fresh.data ?? []).filter((r): r is UniboxListRow => r != null);
    // Everything fits in one page now, so nothing older needs keeping.
    if (!fresh.pagination.has_more) return { pages: [fresh], pageParams: [null] };
    const fresher = new Set(rows.map(rowKey));
    const last = rows[rows.length - 1];
    const [head, ...rest] = old.pages;
    // A head row newer than the fresh page's end but missing from it has left the list.
    const kept = (head.data ?? []).filter((r) => r != null && !fresher.has(rowKey(r)) && (!last || olderThan(r, last)));
    return {
        pageParams: old.pageParams,
        pages: [
            // The old cursor still continues after the old head's last row, which `kept` keeps.
            { ...head, data: [...rows, ...kept] },
            ...rest.map((page) => ({ ...page, data: (page.data ?? []).filter((r) => r != null && !fresher.has(rowKey(r))) })),
        ],
    };
}

async function refreshHead(queryClient: QueryClient, query: Query) {
    const [, , params] = query.queryKey as ["unibox", "search", UniboxSearchParams, string];
    const before = query.state.dataUpdatedAt;
    try {
        const fresh = await searchIncoming({ ...params, cursor: undefined, limit: HEAD_LIMIT });
        // Someone edited the list meanwhile (a row filed away); splicing an older read would put it back.
        if (queryClient.getQueryState(query.queryKey)?.dataUpdatedAt !== before) return;
        queryClient.setQueryData<SearchData>(query.queryKey, (old) => mergeHead(old, fresh));
        void queryClient.invalidateQueries({ queryKey: query.queryKey, exact: true, refetchType: "none" });
    } catch {
        // A failed head read falls back to the whole refetch it replaced.
        void queryClient.invalidateQueries({ queryKey: query.queryKey, exact: true });
    }
}

/**
 * An Invalidate for the refresh coalescer: a visible list with several pages
 * loaded refreshes its head; everything else is invalidated as before.
 */
export function createListHeadRefresh(queryClient: QueryClient, fallback: Invalidate): Invalidate {
    return (filters: InvalidateQueryFilters, options) => {
        const hidden = typeof document !== "undefined" && document.visibilityState === "hidden";
        const heads: Query[] = [];
        const headHashes = new Set<string>();
        if (!hidden) {
            for (const query of queryClient.getQueryCache().findAll({ ...filters, type: "active" })) {
                const data = query.state.data as SearchData | undefined;
                const [root, kind] = query.queryKey as unknown[];
                if (root === "unibox" && kind === "search" && (data?.pages.length ?? 0) > 1) {
                    heads.push(query);
                    headHashes.add(query.queryHash);
                }
            }
        }
        const match = filters.predicate;
        fallback({ ...filters, predicate: (q) => !headHashes.has(q.queryHash) && (!match || match(q)) }, options);
        for (const query of heads) void refreshHead(queryClient, query);
    };
}
