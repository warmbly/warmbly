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
  scopeLabel: string;
  params: UniboxSearchParams;
  setParams: React.Dispatch<React.SetStateAction<UniboxSearchParams>>;
}

export function ConversationList({
  scopeLabel,
  params,
  setParams,
}: ConversationListProps) {
  const [search, setSearch] = React.useState("");
  const [sheetOpen, setSheetOpen] = React.useState(false);
  const searchRef = React.useRef<HTMLInputElement>(null);
  const listRef = React.useRef<HTMLDivElement>(null);
  const selectedThreadId = useAppStore((s) => s.selectedThreadId);
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);
  const setSelectedAccountId = useAppStore((s) => s.setSelectedAccountId);

  const merged: UniboxSearchParams = React.useMemo(() => {
    const next: UniboxSearchParams = { ...params };
    if (search.trim()) next.query = search.trim();
    return next;
  }, [params, search]);

  const q = useUniboxSearch(merged);
  const emails = q.emails;
  const totalShown = emails.length;

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

  // Keyboard navigation. We work off `emails` (flat order) so j/k
  // moves across bucket boundaries naturally. Ignoring shortcuts
  // while typing into any input/textarea, and while the filter
  // sheet is open, keeps the bindings out of the user's way.
  React.useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (sheetOpen) return;
      const target = e.target as HTMLElement | null;
      if (target) {
        const tag = target.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || target.isContentEditable) {
          // Allow Escape from search to deselect.
          if (e.key === "Escape" && target === searchRef.current) {
            searchRef.current?.blur();
          }
          return;
        }
      }

      const currentIdx = selectedThreadId
        ? emails.findIndex(
            (row) => (row.thread_id || row.id) === selectedThreadId,
          )
        : -1;

      const move = (delta: number) => {
        if (emails.length === 0) return;
        const next = Math.max(
          0,
          Math.min(
            emails.length - 1,
            (currentIdx < 0 ? 0 : currentIdx) + delta,
          ),
        );
        const row = emails[next];
        if (!row) return;
        setSelectedThreadId(row.thread_id || row.id);
        setSelectedAccountId(row.email_id ?? null);
        // Bring the focused row into view if the list scrolled.
        requestAnimationFrame(() => {
          const el = listRef.current?.querySelector<HTMLElement>(
            `[data-thread-id="${row.thread_id || row.id}"]`,
          );
          el?.scrollIntoView({ block: "nearest" });
        });
        e.preventDefault();
      };

      switch (e.key) {
        case "j":
          move(1);
          return;
        case "k":
          move(-1);
          return;
        case "Enter":
          if (currentIdx < 0 && emails[0]) {
            const row = emails[0];
            setSelectedThreadId(row.thread_id || row.id);
            setSelectedAccountId(row.email_id ?? null);
            e.preventDefault();
          }
          return;
        case "Escape":
          if (selectedThreadId) {
            setSelectedThreadId(null);
            setSelectedAccountId(null);
            e.preventDefault();
          }
          return;
        case "/":
          searchRef.current?.focus();
          e.preventDefault();
          return;
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [
    emails,
    selectedThreadId,
    sheetOpen,
    setSelectedThreadId,
    setSelectedAccountId,
  ]);

  return (
    <div className="flex flex-col h-full bg-white">
      <div className="h-9 px-2 shrink-0 border-b border-slate-200 flex items-center gap-1.5">
        <SearchIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
        <input
          ref={searchRef}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
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
        {q.isPending ? (
          <SkeletonRows />
        ) : q.isError ? (
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
                          body: e.snippet,
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
            {q.hasNextPage && (
              <div className="px-3 py-3 flex justify-center border-t border-slate-200/60">
                <button
                  onClick={() => q.fetchNextPage()}
                  disabled={q.isFetchingNextPage}
                  className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                >
                  {q.isFetchingNextPage ? (
                    <>
                      <Loader2Icon className="w-3 h-3 animate-spin" />
                      Loading…
                    </>
                  ) : (
                    `Load more · ${totalShown} shown`
                  )}
                </button>
              </div>
            )}
          </>
        )}
      </div>

      {/* Keyboard cheat-sheet footer. Slim and unobtrusive but
                makes the shortcuts discoverable without a help menu. */}
      <div className="h-6 px-2 shrink-0 border-t border-slate-200/80 bg-slate-50/60 flex items-center gap-2 text-[10px] text-slate-500 overflow-x-auto">
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
