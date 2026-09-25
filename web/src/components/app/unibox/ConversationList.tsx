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
import { CheckIcon, PanelLeftIcon, PenLineIcon, SearchIcon } from "lucide-react";
import { AnimatedRow } from "./AnimatedRow";
import { ConversationItem } from "./ConversationItem";
import { SelectionBar } from "./SelectionBar";
import { useConversationActions } from "@/hooks/useConversationActions";
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
import { Checkbox } from "@/components/ui/checkbox";

type Bucket = "today" | "yesterday" | "week" | "earlier";

const BUCKET_LABELS: Record<Bucket, string> = {
  today: "Today",
  yesterday: "Yesterday",
  week: "This week",
  earlier: "Earlier",
};

// Views where an empty list means the work is done rather than that nothing
// ever landed there.
const CAUGHT_UP_SCOPES = new Set(["folder:inbox", "unread", "awaiting", "all"]);

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
  /** Identity of the current scope. */
  scopeKey: string;
  scopeLabel: string;
  params: UniboxSearchParams;
  /** What the scope alone queries; anything beyond it is a user filter. */
  baseParams: UniboxSearchParams;
  setParams: React.Dispatch<React.SetStateAction<UniboxSearchParams>>;
  /**
   * The search box. Owned by the page rather than here, so widening a search
   * to every folder can switch scope without throwing away what was typed.
   */
  search: string;
  setSearch: (value: string) => void;
  /** Widen to every folder, keeping the query. Absent when already there. */
  onSearchAllMail?: () => void;
  /** Opens the mobile view switcher; the rail is hidden below lg. */
  onOpenScopeSheet?: () => void;
}

export function ConversationList({
  scopeKey,
  scopeLabel,
  params,
  baseParams,
  setParams,
  search,
  setSearch,
  onSearchAllMail,
  onOpenScopeSheet,
}: ConversationListProps) {
  const [filtersOpen, setFiltersOpen] = React.useState(false);

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

  const q = useUniboxSearch(merged, scopeKey);
  const emails = q.emails;
  const totalShown = emails.length;
  const activeFilters = countUserFilters(params, baseParams);

  // ── Multi-select ───────────────────────────────────────────────
  // Thread ids, not row indexes: the list re-orders under a refetch, and an
  // index would then name a different conversation than the one ticked.
  const actions = useConversationActions();
  const [picked, setPicked] = React.useState<ReadonlySet<string>>(
    () => new Set<string>(),
  );
  // `selectMode` is the touch entry point, where there is no hover to reveal a
  // checkbox with. It stays on after the last row is unticked, so unticking
  // one by mistake does not take every box off screen.
  const [selectMode, setSelectMode] = React.useState(false);
  // A new scope is a new set of rows; carrying a selection across would apply
  // an action to conversations the user can no longer see.
  const [pickedScope, setPickedScope] = React.useState(scopeKey);
  if (pickedScope !== scopeKey) {
    setPickedScope(scopeKey);
    setPicked(new Set<string>());
    setSelectMode(false);
  }

  const rowKey = React.useCallback(
    (row: (typeof emails)[number]) => row.thread_id || row.id,
    [],
  );
  // What the bar acts on: only rows still on screen. A conversation that has
  // left the list (filed by a teammate, snoozed, filtered out) must not be
  // counted, or the bar promises an action on something nobody can see.
  const selectedIds = React.useMemo(
    () => emails.map(rowKey).filter((id) => picked.has(id)),
    [emails, picked, rowKey],
  );
  const allSelected = emails.length > 0 && selectedIds.length === emails.length;
  const selecting = selectMode || selectedIds.length > 0;
  const clearSelection = React.useCallback(() => {
    setPicked(new Set<string>());
    setSelectMode(false);
  }, []);

  // Shift extends from the last row ticked, the way a file list does.
  const lastPicked = React.useRef<string | null>(null);
  const toggleSelect = React.useCallback(
    (threadId: string, next: boolean, extend: boolean) => {
      // Read the anchor before the updater, not inside it. React may defer an
      // updater to render time, by which point the assignment below has
      // already moved the anchor onto the row being clicked, and a shift-click
      // would extend a range from a row to itself.
      const anchor = lastPicked.current;
      setPicked((prev) => {
        const out = new Set(prev);
        const from = anchor ? emails.findIndex((r) => rowKey(r) === anchor) : -1;
        const to = emails.findIndex((r) => rowKey(r) === threadId);
        if (extend && from >= 0 && to >= 0) {
          const [lo, hi] = from < to ? [from, to] : [to, from];
          for (let i = lo; i <= hi; i++) {
            if (next) out.add(rowKey(emails[i]));
            else out.delete(rowKey(emails[i]));
          }
        } else if (next) {
          out.add(threadId);
        } else {
          out.delete(threadId);
        }
        return out;
      });
      lastPicked.current = threadId;
    },
    [emails, rowKey],
  );

  const toggleAll = React.useCallback(() => {
    setPicked((prev) => {
      const everything = emails.map(rowKey);
      const all = everything.length > 0 && everything.every((id) => prev.has(id));
      return all ? new Set<string>() : new Set(everything);
    });
    lastPicked.current = null;
  }, [emails, rowKey]);

  // A search or filter change keeps the previous rows on screen while
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

  // Rows and their time-bucket headers as one flat sequence, so a single
  // AnimatePresence sees every entry and exit (a header leaves with the last
  // row under it). The server orders newest first, so one pass keeps both
  // the order and the grouping.
  const items = React.useMemo(() => {
    const out: (
      | { kind: "header"; key: string; bucket: Bucket }
      | { kind: "row"; key: string; row: (typeof emails)[number] }
    )[] = [];
    let last: Bucket | null = null;
    for (const e of emails) {
      const b = bucketFor(new Date(e.internal_date));
      if (b !== last) out.push({ kind: "header", key: `bucket:${b}`, bucket: b });
      last = b;
      out.push({ kind: "row", key: rowKey(e), row: e });
    }
    return out;
  }, [emails, rowKey]);

  // Which items were on screen last time, and a generation per key that
  // left. A key that comes back gets a fresh presence key: AnimatePresence
  // unmounts exiting children only when every one has finished, and one that
  // re-enters mid-exit never reports, stranding the rest of the batch (a
  // date header when the list refills, rows restored by a failed action).
  const itemSig = React.useMemo(() => items.map((i) => i.key).join("\n"), [items]);
  // A new result set remounts every row, so nothing in it counts as kept.
  const resultSet = shownKey.current;
  const [shown, setShown] = React.useState<{
    sig: string;
    set: string;
    prev: ReadonlySet<string>;
    gen: ReadonlyMap<string, number>;
  }>(() => ({ sig: "", set: resultSet, prev: new Set<string>(), gen: new Map() }));
  if (shown.sig !== itemSig || shown.set !== resultSet) {
    const sameSet = shown.set === resultSet;
    const prev = sameSet && shown.sig ? shown.sig.split("\n") : [];
    const now = new Set(itemSig ? itemSig.split("\n") : []);
    const gen = new Map(sameSet ? shown.gen : []);
    for (const k of prev) if (!now.has(k)) gen.set(k, (gen.get(k) ?? 0) + 1);
    setShown({ sig: itemSig, set: resultSet, prev: new Set(prev), gen });
  }
  const presenceKey = (key: string) => {
    const g = shown.gen.get(key);
    return g ? `${key}~${g}` : key;
  };

  // How each new row should appear. One inserted above rows already on
  // screen (an arrival, an Undo) grows into place; rows added below them (a
  // page, a refill after a bulk action) fade up in a short stagger and take
  // their full height at once, so the infinite-scroll sentinel is pushed out
  // of range immediately instead of chaining every page in behind them.
  const entering = React.useMemo(() => {
    const keys = itemSig ? itemSig.split("\n") : [];
    let lastKept = -1;
    keys.forEach((k, i) => {
      if (shown.prev.has(k)) lastKept = i;
    });
    const out = new Map<string, { arrival: boolean; rank: number }>();
    let rank = 0;
    keys.forEach((k, i) => {
      if (!shown.prev.has(k)) out.set(k, { arrival: i < lastKept, rank: rank++ });
    });
    return out;
  }, [itemSig, shown.prev]);

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
        // The ticks are the innermost thing Escape can clear: taking the open
        // conversation away first would leave a selection bar with no obvious
        // way to dismiss it.
        if (selecting) {
          clearSelection();
          return;
        }
        if (!selectedThreadId) return;
        setSelectedThreadId(null);
        setSelectedAccountId(null);
      },
      listToggleSelect: () => {
        const row = emails[currentIndex()];
        if (!row) return;
        const id = rowKey(row);
        toggleSelect(id, !picked.has(id), false);
      },
      listArchive: () => {
        // The ticked rows when there are any, otherwise the focused one.
        const target = selectedIds.length > 0 ? selectedIds : [];
        if (target.length === 0) {
          const row = emails[currentIndex()];
          if (!row) return;
          void actions.file([rowKey(row)], "archive");
          return;
        }
        void actions.file(target, "archive").finally(clearSelection);
      },
      focusSearch: () => searchRef.current?.focus(),
    },
    // The filter popover owns the keyboard while it is open.
    { suspended: filtersOpen },
  );

  const filtering = activeFilters > 0 || !!search.trim();
  const rowScope = scopeKey.startsWith("folder:")
    ? scopeKey.slice("folder:".length)
    : scopeKey;

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
        {/* Select is the touch entry point into multi-select; on a pointer
            device hovering a row already shows its box, but the control is
            kept everywhere so the feature is discoverable at all. */}
        {selecting ? (
          <label className="h-7 px-2 rounded-md inline-flex items-center gap-1.5 text-[11.5px] text-slate-600 hover:bg-slate-100 cursor-pointer transition-colors shrink-0">
            <Checkbox
              checked={allSelected}
              onChange={toggleAll}
              aria-label={allSelected ? "Deselect all" : "Select all loaded"}
            />
            All
          </label>
        ) : (
          <button
            type="button"
            onClick={() => setSelectMode(true)}
            className="h-7 px-2 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
          >
            Select
          </button>
        )}
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
            // The box searches people, subject and message body, and naming
            // that is the difference between it looking broken and looking
            // useful: nobody tries an address in a box labelled "Search inbox".
            placeholder={`Search ${scopeLabel.toLowerCase()}: name, address, or any word`}
            title={'Searches the sender, recipients, subject and message body. "quoted phrases", OR and -exclude work.'}
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
          // The x clip keeps a row sliding out from drawing a scrollbar.
          "flex-1 overflow-y-auto overflow-x-hidden transition-opacity duration-200",
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
        ) : (
          <React.Fragment key={shownKey.current}>
            <AnimatePresence>
              {items.map((item) =>
                item.kind === "header" ? (
                  <motion.div
                    key={presenceKey(item.key)}
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1, height: 28, transition: { duration: 0.2 } }}
                    // Leaves after the rows under it have slid away.
                    exit={{ opacity: 0, height: 0, transition: { duration: 0.2, delay: 0.24 } }}
                    className="sticky top-0 z-10 px-4 h-7 bg-white/95 backdrop-blur-sm flex items-center overflow-hidden"
                  >
                    <span className="text-[10.5px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                      {BUCKET_LABELS[item.bucket]}
                    </span>
                  </motion.div>
                ) : (
                  <AnimatedRow
                    key={presenceKey(item.key)}
                    motionKey={item.key}
                    custom={{
                      id: item.key,
                      arrival: entering.get(item.key)?.arrival ?? false,
                      rank: entering.get(item.key)?.rank ?? 0,
                    }}
                  >
                    <ConversationItem
                      // The row's actions read the scope: Archive and Trash
                      // offer the way back rather than the way out.
                      scope={rowScope}
                      selected={picked.has(item.key)}
                      selecting={selecting}
                      onToggleSelect={toggleSelect}
                      actions={actions}
                      email={{
                        id: item.row.id,
                        from: item.row.from_addr?.[0] ?? "",
                        to: item.row.to_addr?.[0] ?? "",
                        subject: item.row.subject,
                        snippet: item.row.snippet,
                        date: new Date(item.row.internal_date),
                        // Bold the whole conversation when any message in
                        // the thread is unread.
                        is_seen: !item.row.has_unread,
                        thread_id: item.row.thread_id,
                        account_id: item.row.email_id,
                        message_count: item.row.message_count,
                        labels: item.row.labels,
                      }}
                    />
                  </AnimatedRow>
                ),
              )}
            </AnimatePresence>
            {/* Mounted beside the rows rather than instead of them, so the last
                ones can still animate out when an action empties the list. It
                waits for them before it appears. */}
            <AnimatePresence>
              {emails.length === 0 && !hasNextPage && (
                <motion.div
                  key="empty"
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0, transition: { duration: 0.28, delay: 0.3, ease: [0.16, 1, 0.3, 1] } }}
                  exit={{ opacity: 0, transition: { duration: 0.1 } }}
                >
                  <EmptyState
                    caughtUp={!filtering && CAUGHT_UP_SCOPES.has(scopeKey)}
                    filtering={filtering}
                    search={search}
                    scopeLabel={scopeLabel}
                    onSearchAllMail={onSearchAllMail}
                  />
                </motion.div>
              )}
            </AnimatePresence>
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

      <AnimatePresence>
        {selectedIds.length > 0 && (
          <SelectionBar
            key="selection"
            threadIds={selectedIds}
            actions={actions}
            scope={rowScope}
            onClear={clearSelection}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

function EmptyState({
  caughtUp,
  filtering,
  search,
  scopeLabel,
  onSearchAllMail,
}: {
  caughtUp: boolean;
  filtering: boolean;
  search: string;
  scopeLabel: string;
  onSearchAllMail?: () => void;
}) {
  if (caughtUp) {
    return (
      <div className="px-5 py-16 text-center">
        <motion.div
          initial={{ scale: 0.6, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          transition={{ type: "spring", stiffness: 380, damping: 22, delay: 0.36 }}
          className="mx-auto mb-3 size-9 rounded-full bg-emerald-50 text-emerald-600 inline-flex items-center justify-center"
        >
          <CheckIcon className="w-4 h-4" strokeWidth={2.25} />
        </motion.div>
        <p className="text-[12.5px] text-slate-700 font-medium mb-1">
          All caught up
        </p>
        <p className="text-[11.5px] text-slate-400 max-w-[32ch] mx-auto leading-relaxed">
          New mail shows up here as it arrives.
        </p>
      </div>
    );
  }
  return (
    <div className="px-5 py-16 text-center">
      <p className="text-[12.5px] text-slate-700 font-medium mb-1">
        {filtering ? "No matches" : "Nothing here"}
      </p>
      <p className="text-[11.5px] text-slate-400 max-w-[32ch] mx-auto leading-relaxed">
        {filtering
          ? search.trim()
            ? `Nothing in ${scopeLabel.toLowerCase()} matches "${search.trim()}". The search covers names, addresses, subjects and message bodies.`
            : "Try a different search or clear the filters."
          : "New mail shows up here as it arrives."}
      </p>
      {/* The commonest reason a search finds nothing is that the thing is
          filed somewhere else. Offer the wider search rather than quietly
          overriding the scope the reader chose. */}
      {filtering && search.trim() && onSearchAllMail && (
        <button
          type="button"
          onClick={onSearchAllMail}
          className="mt-3 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
        >
          <SearchIcon className="w-3 h-3" />
          Search all mail
        </button>
      )}
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
