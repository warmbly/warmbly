// Middle pane of the unibox.
//
// Three upgrades over the flat list:
//   1. Time-bucket grouping (Today / Yesterday / This week / Earlier)
//      with sticky group headers so scanning across days reads as a
//      timeline, not a uniform wall.
//   2. Keyboard navigation: j/k step through rows, Enter opens, Esc
//      deselects, e archives (placeholder), r focuses reply.
//   3. A compact footer with the keyboard cheat-sheet so the
//      shortcuts are discoverable without a help menu.
//
// All scope/filter state still lives in the parent page — this
// component owns only its own search box + focused row.

import React from "react";
import { Loader2Icon, SearchIcon, Settings2Icon } from "lucide-react";
import { ConversationItem } from "./ConversationItem";
import useUniboxSearch from "@/lib/api/hooks/app/unibox/useUniboxSearch";
import { useShortcutActions } from "@/hooks/useShortcutActions";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useScrollMemory } from "@/hooks/useScrollMemory";
import { useAppStore } from "@/stores";
import { UniboxFilterSheet } from "./UniboxFilterSheet";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

type Bucket = "today" | "yesterday" | "week" | "earlier";

const BUCKET_LABELS: Record<Bucket, string> = {
  today: "Today",
  yesterday: "Yesterday",
  week: "This week",
  earlier: "Earlier",
};

function bucketFor(d: Date): Bucket {
  const now = new Date();
  const today = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
  ).getTime();
  const yesterday = today - 24 * 60 * 60 * 1000;
  const weekStart = today - 6 * 24 * 60 * 60 * 1000;
  const t = d.getTime();
  if (t >= today) return "today";
  if (t >= yesterday) return "yesterday";
  if (t >= weekStart) return "week";
  return "earlier";
}

interface ConversationListProps {
  /** Identity of the current scope; a change clears the local search. */
  scopeKey: string;
  scopeLabel: string;
  params: UniboxSearchParams;
  setParams: React.Dispatch<React.SetStateAction<UniboxSearchParams>>;
}

export function ConversationList({
  scopeKey,
  scopeLabel,
  params,
  setParams,
}: ConversationListProps) {
  const [search, setSearch] = React.useState("");
  const [sheetOpen, setSheetOpen] = React.useState(false);

  // The page keeps this component mounted across a scope switch (that is what
  // holds the scroll offset when a thread opens), so the search box has to be
  // cleared here or a query typed for one scope would silently filter the next.
  // Set during render, like the page's own param reset, so the stale query
  // never reaches the request.
  const [searchScope, setSearchScope] = React.useState(scopeKey);
  if (searchScope !== scopeKey) {
    setSearchScope(scopeKey);
    setSearch("");
  }

  const searchRef = React.useRef<HTMLInputElement>(null);
  const listRef = React.useRef<HTMLDivElement>(null);
  const sentinelRef = React.useRef<HTMLDivElement>(null);
  const selectedThreadId = useAppStore((s) => s.selectedThreadId);
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);
  const setSelectedAccountId = useAppStore((s) => s.setSelectedAccountId);

  // Debounced into the query, immediate in the box: the search text is part of
  // the query key, so a raw binding fires a request and parks a cached page per
  // keystroke.
  const debouncedSearch = useDebouncedValue(search);
  const merged: UniboxSearchParams = React.useMemo(() => {
    const next: UniboxSearchParams = { ...params };
    if (debouncedSearch.trim()) next.query = debouncedSearch.trim();
    return next;
  }, [params, debouncedSearch]);

  const q = useUniboxSearch(merged);
  const emails = q.emails;
  const totalShown = emails.length;

  // Where this exact list was left. Opening a thread keeps the page mounted
  // (see the route's stableParams in main.tsx), so this covers what that
  // cannot: leaving the inbox and coming back, and the mobile pane, which the
  // browser scrolls to the top while it is display:none.
  const listKey = React.useMemo(() => JSON.stringify(merged), [merged]);
  useScrollMemory(listRef, listKey);

  // Infinite scroll: reaching the end of the list loads the next page instead
  // of asking for a click. The button below stays as the manual fallback, and
  // isFetchingNextPage is a dependency so a page landing re-arms the observer:
  // a sentinel still on screen keeps pulling instead of stalling one page in.
  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = q;
  React.useEffect(() => {
    const sentinel = sentinelRef.current;
    const root = listRef.current;
    // A page that failed stays failed until the user asks again; re-arming on
    // an on-screen sentinel would retry it on a loop.
    if (!sentinel || !root || !hasNextPage || isFetchNextPageError) return;
    if (typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) void fetchNextPage();
      },
      { root, rootMargin: "400px 0px" },
    );
    io.observe(sentinel);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);

  // Group rows by time bucket. The server already orders newest →
  // oldest so a single pass preserves both global order and group
  // adjacency.
  const grouped = React.useMemo(() => {
    const groups: { bucket: Bucket; rows: typeof emails }[] = [];
    for (const e of emails) {
      const b = bucketFor(new Date(e.internal_date));
      const tail = groups[groups.length - 1];
      if (tail && tail.bucket === b) tail.rows.push(e);
      else groups.push({ bucket: b, rows: [e] });
    }
    return groups;
  }, [emails]);

  // Keyboard navigation. We work off `emails` (flat order) so j/k moves across
  // bucket boundaries naturally. The keys themselves live in the global
  // registry (useKeyboardShortcuts); this only says what they mean here, so the
  // `?` modal and the dispatcher cannot disagree about whether they work.
  const selectRow = React.useCallback(
    (row: (typeof emails)[number] | undefined) => {
      if (!row) return;
      const id = row.thread_id || row.id;
      setSelectedThreadId(id);
      setSelectedAccountId(row.email_id ?? null);
      // Bring the newly selected row into view if the list scrolled.
      requestAnimationFrame(() => {
        const el = listRef.current?.querySelector<HTMLElement>(
          `[data-thread-id="${id}"]`,
        );
        el?.scrollIntoView({ block: "nearest" });
      });
    },
    [setSelectedThreadId, setSelectedAccountId],
  );

  const currentIndex = React.useCallback(
    () =>
      selectedThreadId
        ? emails.findIndex((row) => (row.thread_id || row.id) === selectedThreadId)
        : -1,
    [emails, selectedThreadId],
  );

  useShortcutActions(
    {
      listMove: (delta) => {
        if (emails.length === 0) return;
        const from = currentIndex();
        // Nothing selected yet: j takes the top of the list and k the bottom.
        // Treating "no selection" as index 0 made the first j skip the row the
        // user was already looking at.
        if (from < 0) {
          selectRow(delta > 0 ? emails[0] : emails[emails.length - 1]);
          return;
        }
        selectRow(
          emails[Math.max(0, Math.min(emails.length - 1, from + delta))],
        );
      },
      listEdge: (edge) =>
        selectRow(edge === "first" ? emails[0] : emails[emails.length - 1]),
      listOpen: () => {
        // A row is opened by selecting it; Enter is only meaningful before
        // anything is selected, where it takes the top of the list.
        if (currentIndex() < 0) selectRow(emails[0]);
      },
      listDeselect: () => {
        if (!selectedThreadId) return;
        setSelectedThreadId(null);
        setSelectedAccountId(null);
      },
      focusSearch: () => searchRef.current?.focus(),
    },
    // The filter sheet owns the keyboard while it is open.
    { suspended: sheetOpen },
  );

  return (
    <div className="flex flex-col h-full bg-white">
      <div className="h-9 px-2 shrink-0 border-b border-slate-200 flex items-center gap-1.5">
        <SearchIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
        <input
          ref={searchRef}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          // Escape gives the keyboard back to the list instead of bubbling up
          // to the global dispatcher, which ignores keys typed into an input.
          onKeyDown={(e) => {
            if (e.key === "Escape") e.currentTarget.blur();
          }}
          placeholder={`Search ${scopeLabel.toLowerCase()}… (/)`}
          className="flex-1 min-w-0 h-7 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
        />
        {totalShown > 0 && (
          <span className="font-mono tabular-nums text-[10.5px] text-slate-400 shrink-0">
            {totalShown}
          </span>
        )}
        <button
          type="button"
          onClick={() => setSheetOpen(true)}
          className="size-7 rounded text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
          aria-label="Advanced filters"
        >
          <Settings2Icon className="w-3.5 h-3.5" />
        </button>
      </div>

      <div ref={listRef} className="flex-1 overflow-y-auto">
        {q.isPending && emails.length === 0 ? (
          <SkeletonRows />
        ) : q.isError && emails.length === 0 ? (
          <div className="px-5 py-12 text-center">
            <p className="text-[12.5px] text-slate-900 font-medium mb-1">
              Couldn't load inbox
            </p>
            <p className="text-[11.5px] text-slate-500 mb-3">
              {q.error?.message ?? "Request failed"}
            </p>
            <button
              type="button"
              onClick={() => q.refetch()}
              className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
            >
              Try again
            </button>
          </div>
        ) : emails.length === 0 ? (
          <div className="px-5 py-16 text-center">
            <p className="text-[12.5px] text-slate-700 font-medium mb-1">
              {hasActiveFilters(merged) || search.trim()
                ? "No matches"
                : "Nothing here yet"}
            </p>
            <p className="text-[11.5px] text-slate-400 max-w-[28ch] mx-auto leading-relaxed">
              {hasActiveFilters(merged) || search.trim()
                ? "Try a different scope or clear the filters."
                : "Pick a different scope from the rail, or wait for new mail."}
            </p>
          </div>
        ) : (
          <>
            {grouped.map((g) => (
              <section key={g.bucket}>
                <div className="sticky top-0 z-10 px-3 py-1 bg-slate-50/95 backdrop-blur-sm border-b border-slate-200/60 flex items-center gap-2">
                  <span className="text-[10px] uppercase tracking-[0.14em] text-slate-500 font-semibold">
                    {BUCKET_LABELS[g.bucket]}
                  </span>
                  <span className="font-mono text-[10px] text-slate-400 tabular-nums">
                    {g.rows.length}
                  </span>
                </div>
                <div className="divide-y divide-slate-200/60">
                  {g.rows.map((e) => (
                    <div key={e.id} data-thread-id={e.thread_id || e.id}>
                      <ConversationItem
                        email={{
                          id: e.id,
                          from: e.from_addr?.[0] ?? "",
                          to: e.to_addr?.[0] ?? "",
                          subject: e.subject,
                          snippet: e.snippet,
                          date: new Date(e.internal_date),
                          // Bold the whole conversation when any
                          // message in the thread is unread.
                          is_seen: !e.has_unread,
                          thread_id: e.thread_id,
                          account_id: e.email_id,
                          message_count: e.message_count,
                          labels: e.labels,
                        }}
                      />
                    </div>
                  ))}
                </div>
              </section>
            ))}
            {hasNextPage && (
              <div
                ref={sentinelRef}
                className="px-3 py-3 flex flex-col items-center gap-1.5 border-t border-slate-200/60"
              >
                {isFetchingNextPage ? (
                  <span className="h-7 text-[12px] text-slate-400 inline-flex items-center gap-1.5">
                    <Loader2Icon className="w-3 h-3 animate-spin" />
                    Loading more…
                  </span>
                ) : (
                  <>
                    {isFetchNextPageError && (
                      <span className="text-[11.5px] text-rose-600">
                        Couldn't load more conversations
                      </span>
                    )}
                    <button
                      onClick={() => fetchNextPage()}
                      className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                    >
                      {isFetchNextPageError
                        ? "Try again"
                        : `Load more · ${totalShown} shown`}
                    </button>
                  </>
                )}
              </div>
            )}
          </>
        )}
      </div>

      {/* Keyboard cheat-sheet footer. Slim and unobtrusive but
                makes the shortcuts discoverable without a help menu. */}
      <div className="h-6 px-2 shrink-0 border-t border-slate-200/80 bg-slate-50/60 hidden md:flex items-center gap-2 text-[10px] text-slate-500 overflow-x-auto">
        <Kbd>j</Kbd>/<Kbd>k</Kbd>
        <span className="text-slate-400">move</span>
        <Kbd>↵</Kbd>
        <span className="text-slate-400">open</span>
        <Kbd>esc</Kbd>
        <span className="text-slate-400">close</span>
        <Kbd>/</Kbd>
        <span className="text-slate-400">search</span>
      </div>

      <UniboxFilterSheet
        open={sheetOpen}
        setOpen={setSheetOpen}
        filters={params}
        setFilters={setParams}
        loading={q.isFetching}
      />
    </div>
  );
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="px-1 h-3.5 rounded-sm bg-white border border-slate-200 text-slate-600 font-mono text-[9px] inline-flex items-center shrink-0">
      {children}
    </kbd>
  );
}

function hasActiveFilters(p: UniboxSearchParams): boolean {
  return Boolean(
    p.from ||
    (p.accountIds && p.accountIds.length > 0) ||
    p.tagId ||
    (p.categoryIds && p.categoryIds.length > 0) ||
    p.unseen !== undefined ||
    p.since ||
    p.until ||
    p.snoozed ||
    p.awaitingReply,
  );
}

function SkeletonRows() {
  return (
    <div className="divide-y divide-slate-200/60">
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="px-3 py-2.5 flex items-center gap-2.5">
          <div className="size-7 rounded-full bg-slate-100 shrink-0" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex items-center gap-2">
              <div className="h-2.5 w-32 bg-slate-100 rounded animate-pulse" />
              <div className="ml-auto h-2.5 w-8 bg-slate-100 rounded animate-pulse" />
            </div>
            <div className="h-2.5 w-44 bg-slate-100 rounded animate-pulse" />
            <div className="h-2.5 w-56 bg-slate-100 rounded animate-pulse" />
          </div>
        </div>
      ))}
    </div>
  );
}
