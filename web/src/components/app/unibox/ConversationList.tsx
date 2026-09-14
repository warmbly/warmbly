// Middle pane of the unibox.
//
// A title row (the scope, its count, the filter button; on phones also the
// view switcher and Compose, since the rail is hidden there), a search row,
// then the rows grouped under quiet Today / Yesterday / This week / Earlier
// headers. Keyboard: j/k step through rows, Enter opens, Esc deselects, and
// `/` focuses search; the `?` modal is where they are listed.
//
// All scope/filter state still lives in the parent page; this component owns
// only its own search box and focused row.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { PanelLeftIcon, PenLineIcon, SearchIcon } from "lucide-react";
import { ConversationItem } from "./ConversationItem";
import useUniboxSearch from "@/lib/api/hooks/app/unibox/useUniboxSearch";
import { useShortcutActions } from "@/hooks/useShortcutActions";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useScrollMemory } from "@/hooks/useScrollMemory";
import { useAppStore } from "@/stores";
import { useComposeStore } from "@/hooks/useComposeStore";
import {
  countUserFilters,
  UniboxFilterButton,
  UniboxFilterChips,
} from "./UniboxFilterPopover";
import { cn } from "@/lib/utils";
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
  /** What the scope alone queries; anything beyond it is a user filter. */
  baseParams: UniboxSearchParams;
  setParams: React.Dispatch<React.SetStateAction<UniboxSearchParams>>;
  /** Opens the mobile view switcher; the rail is hidden below lg. */
  onOpenScopeSheet?: () => void;
}

export function ConversationList({
  scopeKey,
  scopeLabel,
  params,
  baseParams,
  setParams,
  onOpenScopeSheet,
}: ConversationListProps) {
  const [search, setSearch] = React.useState("");
  const [filtersOpen, setFiltersOpen] = React.useState(false);

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
  // State, not a ref: the rows sit in a keyed fragment that remounts when a
  // new result set replaces the previous one, and the sentinel remounts with
  // it. A ref would leave the observer below watching the detached node and
  // auto-pagination would quietly stop, since none of its other dependencies
  // change when both result sets have a next page.
  const [sentinel, setSentinel] = React.useState<HTMLDivElement | null>(null);
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
  const activeFilters = countUserFilters(params, baseParams);

  // A scope, search or filter change keeps the previous rows on screen while
  // the new ones load (placeholderData). That is the moment to show progress:
  // a bar along the top and the stale rows dimmed. Background refetches from
  // realtime events do not qualify, so nothing flickers while reading. The
  // bar waits 150ms so a fast response never shows it at all.
  const stale = q.isPlaceholderData && q.isFetching;
  const showProgress = useDelayed(stale, 150);
  const firstLoad = (q.isPending || q.isPlaceholderData) && emails.length === 0;

  // Where this exact list was left. Opening a thread keeps the page mounted
  // (see the route's stableParams in main.tsx), so this covers what that
  // cannot: leaving the inbox and coming back, and the mobile pane, which the
  // browser scrolls to the top while it is display:none.
  const listKey = React.useMemo(() => JSON.stringify(merged), [merged]);
  useScrollMemory(listRef, listKey);

  // Rows fold and fade only for changes within one result set (a filed
  // conversation, a new arrival). A whole new set, after a scope or filter
  // change, lands in one go: the row container is keyed on the query whose
  // data is on screen, which lags the request key for as long as the previous
  // rows are standing in, so the swap remounts rather than animates.
  const shownKey = React.useRef(listKey);
  if (!q.isPlaceholderData) shownKey.current = listKey;

  // Infinite scroll: reaching the end of the list loads the next page instead
  // of asking for a click. The button below stays as the manual fallback, and
  // isFetchingNextPage is a dependency so a page landing re-arms the observer:
  // a sentinel still on screen keeps pulling instead of stalling one page in.
  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = q;
  React.useEffect(() => {
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
  }, [
    sentinel,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  // Group rows by time bucket. The server already orders newest to oldest so a
  // single pass preserves both global order and group adjacency.
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
    // The filter popover owns the keyboard while it is open.
    { suspended: filtersOpen },
  );

  const filtering = activeFilters > 0 || !!search.trim();

  return (
    <div className="relative flex flex-col h-full bg-white">
      <ProgressBar active={showProgress} />
      <div className="h-11 pl-3 pr-2 shrink-0 flex items-center gap-1.5">
        {onOpenScopeSheet && (
          <button
            type="button"
            onClick={onOpenScopeSheet}
            aria-label="Switch view"
            className="lg:hidden size-7 -ml-1 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
          >
            <PanelLeftIcon className="w-4 h-4" />
          </button>
        )}
        <h1 className="text-[13.5px] font-semibold text-slate-900 truncate min-w-0">
          {scopeLabel}
        </h1>
        {totalShown > 0 && (
          <span
            className={cn(
              "tabular-nums text-[11.5px] shrink-0 transition-opacity",
              stale ? "text-slate-300" : "text-slate-400",
            )}
          >
            {totalShown}
            {hasNextPage ? "+" : ""}
          </span>
        )}
        <span className="flex-1" />
        <UniboxFilterButton
          params={params}
          base={baseParams}
          setParams={setParams}
          open={filtersOpen}
          onOpenChange={setFiltersOpen}
        />
        {/* Desktop has the rail's Compose button; this is the phone and tablet
            entry, where the rail is hidden. */}
        <button
          type="button"
          onClick={() => useComposeStore.getState().openCompose()}
          aria-label="New email"
          className="lg:hidden size-7 rounded-md bg-sky-600 hover:bg-sky-700 text-white inline-flex items-center justify-center transition-colors shrink-0"
        >
          <PenLineIcon className="w-3.5 h-3.5" />
        </button>
      </div>

      <div className="px-3 pb-2 shrink-0 border-b border-slate-200">
        <div className="h-8 px-2.5 rounded-md bg-slate-100/80 flex items-center gap-2 focus-within:bg-white focus-within:ring-1 focus-within:ring-sky-300 transition-[background-color,box-shadow]">
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
            placeholder={`Search ${scopeLabel.toLowerCase()}`}
            className="flex-1 min-w-0 h-full bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
          />
          {search ? (
            <button
              type="button"
              onClick={() => setSearch("")}
              className="text-[11px] text-slate-400 hover:text-slate-700 shrink-0"
            >
              Clear
            </button>
          ) : (
            <kbd className="hidden md:inline-flex h-4 px-1 items-center rounded border border-slate-200 bg-white text-[10px] text-slate-400 font-mono shrink-0">
              /
            </kbd>
          )}
        </div>
        <UniboxFilterChips
          params={params}
          base={baseParams}
          setParams={setParams}
          onOpen={() => setFiltersOpen(true)}
        />
      </div>

      <div
        ref={listRef}
        className={cn(
          "flex-1 overflow-y-auto transition-opacity duration-200",
          stale && emails.length > 0 && "opacity-50",
        )}
        aria-busy={stale || undefined}
      >
        {firstLoad ? (
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
              {filtering ? "No matches" : "Nothing here"}
            </p>
            <p className="text-[11.5px] text-slate-400 max-w-[28ch] mx-auto leading-relaxed">
              {filtering
                ? "Try a different search or clear the filters."
                : "New mail shows up here as it arrives."}
            </p>
          </div>
        ) : (
          <React.Fragment key={shownKey.current}>
            {grouped.map((g) => (
              <section key={g.bucket}>
                <div className="sticky top-0 z-10 px-4 h-7 bg-white/95 backdrop-blur-sm flex items-center">
                  <span className="text-[10.5px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                    {BUCKET_LABELS[g.bucket]}
                  </span>
                </div>
                <div className="divide-y divide-slate-100">
                  <AnimatePresence initial={false}>
                  {g.rows.map((e) => (
                    <motion.div
                      key={e.thread_id || e.id}
                      data-thread-id={e.thread_id || e.id}
                      // A row that arrives fades in; one that is filed,
                      // snoozed or deleted folds away instead of vanishing.
                      initial={{ opacity: 0, height: 0 }}
                      animate={{ opacity: 1, height: "auto" }}
                      exit={{ opacity: 0, height: 0 }}
                      transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
                      style={{ overflow: "hidden" }}
                    >
                      <ConversationItem
                        email={{
                          id: e.id,
                          from: e.from_addr?.[0] ?? "",
                          to: e.to_addr?.[0] ?? "",
                          subject: e.subject,
                          snippet: e.snippet,
                          date: new Date(e.internal_date),
                          // Bold the whole conversation when any message in
                          // the thread is unread.
                          is_seen: !e.has_unread,
                          thread_id: e.thread_id,
                          account_id: e.email_id,
                          message_count: e.message_count,
                          labels: e.labels,
                        }}
                      />
                    </motion.div>
                  ))}
                  </AnimatePresence>
                </div>
              </section>
            ))}
            {hasNextPage && (
              <div ref={setSentinel}>
                {isFetchingNextPage ? (
                  // The next page looks like rows before it is rows.
                  <SkeletonRows count={3} />
                ) : (
                  <div className="px-3 py-3 flex flex-col items-center gap-1.5">
                    {isFetchNextPageError && (
                      <span className="text-[11.5px] text-rose-600">
                        Couldn't load more conversations
                      </span>
                    )}
                    <button
                      onClick={() => fetchNextPage()}
                      className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                    >
                      {isFetchNextPageError ? "Try again" : "Load more"}
                    </button>
                  </div>
                )}
              </div>
            )}
          </React.Fragment>
        )}
      </div>

    </div>
  );
}

// True once `active` has held for `ms`, false again the moment it drops, so
// a state that resolves quickly never shows its indicator.
function useDelayed(active: boolean, ms: number): boolean {
  const [shown, setShown] = React.useState(false);
  React.useEffect(() => {
    if (!active) {
      setShown(false);
      return;
    }
    const t = window.setTimeout(() => setShown(true), ms);
    return () => window.clearTimeout(t);
  }, [active, ms]);
  return shown;
}

// A 2px indeterminate bar along the top edge of the column.
function ProgressBar({ active }: { active: boolean }) {
  return (
    <AnimatePresence>
      {active && (
        <motion.div
          key="progress"
          role="progressbar"
          aria-label="Loading conversations"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.15 }}
          className="pointer-events-none absolute inset-x-0 top-0 z-20 h-0.5 overflow-hidden bg-sky-100"
        >
          <motion.div
            className="h-full w-1/3 rounded-full bg-sky-500"
            animate={{ x: ["-100%", "300%"] }}
            transition={{ duration: 1.1, ease: "easeInOut", repeat: Infinity }}
          />
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function SkeletonRows({ count = 8 }: { count?: number }) {
  // Same anatomy as a real row (gutter, three lines, time at the right) so
  // nothing shifts when the rows land. Widths vary per row so it reads as a
  // list rather than a pattern.
  const widths = [
    [30, 52, 64],
    [24, 44, 58],
    [34, 40, 66],
    [28, 56, 60],
    [22, 48, 62],
    [32, 42, 56],
    [26, 50, 64],
    [30, 46, 60],
  ];
  return (
    <div className="divide-y divide-slate-100" aria-hidden>
      {Array.from({ length: count }).map((_, i) => {
        const [a, b, c] = widths[i % widths.length];
        return (
          <div key={i} className="pl-5 pr-4 py-2.5 space-y-[7px]">
            <div className="flex items-center gap-2 h-[18px]">
              <div className="h-2.5 rounded bg-slate-100 animate-pulse" style={{ width: `${a}%` }} />
              <div className="ml-auto h-2.5 w-6 rounded bg-slate-100 animate-pulse" />
            </div>
            <div className="h-2.5 rounded bg-slate-100 animate-pulse" style={{ width: `${b}%` }} />
            <div className="h-2.5 rounded bg-slate-100/80 animate-pulse" style={{ width: `${c}%` }} />
          </div>
        );
      })}
    </div>
  );
}
