// Unibox — three-column overview layout.
//
//   ┌── Top metric strip ─────────────────────────────────────────┐
//   │ Inbox · [scope chip] · unread · awaiting · today · week · …│
//   ├──────────┬────────────────────────┬─────────────────────────┤
//   │  Scope   │ Conversation list      │ Thread (live fetch)     │
//   │  rail    │ (search + dense rows)  │ (deep-linkable URL)     │
//   │ (220px)  │  (drag-resizable)      │  flex-1                 │
//   └──────────┴────────────────────────┴─────────────────────────┘
//
// All counts in the rail and strip come from /unibox/overview in one
// round trip — server truth, no client guesswork. Snoozed and
// Awaiting reply are real backend scopes, not "soon" placeholders.

import React from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ChevronLeftIcon, InboxIcon } from "lucide-react";

import { ConversationList } from "@/components/app/unibox/ConversationList";
import { ScheduledList } from "@/components/app/unibox/ScheduledList";
import { ThreadView } from "@/components/app/unibox/ThreadView";
import { ScopeRail, scopeKey, type UniboxScope } from "@/components/app/unibox/ScopeRail";
import { ScopeSheet } from "@/components/app/unibox/ScopeSheet";
import { UniboxHeader } from "@/components/app/unibox/UniboxHeader";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { LockedSurface } from "@/components/layout/LockedSurface";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import {
  useAppStore,
  UNIBOX_LIST_DEFAULT_WIDTH,
  UNIBOX_LIST_MAX_WIDTH,
  UNIBOX_LIST_MIN_WIDTH,
} from "@/stores";
import { uniboxListMaxWidth, uniboxThreadReserve } from "@/lib/uniboxLayout";
import { useMediaQuery, LG_QUERY } from "@/hooks/useMediaQuery";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import { cn } from "@/lib/utils";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

function startOfToday(): Date {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d;
}

function startOfWeek(): Date {
  const d = startOfToday();
  d.setDate(d.getDate() - 6);
  return d;
}

export default function UniboxPage() {
  const access = useFeatureAccess();
  const canAccess = usePermission("ACCESS_UNIBOX");
  const overview = useUniboxOverview();
  const routeParams = useParams<{ scope?: string; threadId?: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [scopeSheetOpen, setScopeSheetOpen] = React.useState(false);

  // ── Pane widths ────────────────────────────────────────────────
  // The list column is drag-resizable against the thread pane and the width is
  // persisted (warmbly-storage). Two separate bounds apply: the preference's
  // own 280-620 (clamped in the store) and what the viewport can actually give
  // it right now, measured below. The rendered width is the smaller of the two,
  // and that is the number ARIA reports, so the splitter never announces a
  // width the column does not have.
  const listWidth = useAppStore((s) => s.uniboxListWidth);
  const setListWidth = useAppStore((s) => s.setUniboxListWidth);
  const contactRailOpen = useAppStore((s) => s.uniboxContactRailOpen);
  const isWide = useMediaQuery(LG_QUERY);
  const rowRef = React.useRef<HTMLDivElement>(null);
  const listRef = React.useRef<HTMLDivElement>(null);
  const [maxWidth, setMaxWidth] = React.useState(UNIBOX_LIST_MAX_WIDTH);

  // ── URL state ──────────────────────────────────────────────────
  // Readable, path-based URLs: /app/unibox/<scope>[/<threadId>]. The scope is a
  // path segment (all, unread, today, week, awaiting, snoozed, scheduled, or a
  // mailbox/tag/category view); an open thread is the next segment; ref (the
  // opaque mailbox/tag/label id for those scopes) is the only query param left.
  // Accounts are no longer in the URL: the thread fetch scans every mailbox the
  // user owns, which is the right default for a unified inbox.
  const urlScope = routeParams.scope ?? "all";
  const urlThread = routeParams.threadId ?? null;
  const urlScopeRef = searchParams.get("ref");

  // The contact rail is a flex sibling inside the thread pane, so the thread's
  // reserve has to include it whenever it is actually showing.
  const threadReserve = uniboxThreadReserve(
    !!urlThread && isWide && contactRailOpen,
  );

  const measureMax = React.useCallback(() => {
    const row = rowRef.current;
    const list = listRef.current;
    if (!row || !list) return UNIBOX_LIST_MAX_WIDTH;
    return uniboxListMaxWidth({
      rowRight: row.getBoundingClientRect().right,
      listLeft: list.getBoundingClientRect().left,
      reservedForThread: threadReserve,
    });
  }, [threadReserve]);

  // Re-measure on any layout change, not just window resize: collapsing the app
  // nav or opening the contact rail moves the same edges.
  React.useLayoutEffect(() => {
    const row = rowRef.current;
    if (!row) return;
    const sync = () => setMaxWidth(measureMax());
    sync();
    const ro = new ResizeObserver(sync);
    ro.observe(row);
    return () => ro.disconnect();
  }, [measureMax]);

  const renderedWidth = Math.min(listWidth, maxWidth);

  // Drag state. Pointer capture routes every move back to the separator, which
  // matters because the pane being dragged into renders each message body in an
  // iframe: with window listeners the drag dies the moment the cursor crosses
  // one, and the pointerup that would have cleaned up never arrives.
  const dragRef = React.useRef<{ startX: number; startWidth: number; max: number } | null>(null);
  const liveWidthRef = React.useRef(renderedWidth);

  const endDrag = React.useCallback(() => {
    if (!dragRef.current) return;
    dragRef.current = null;
    document.body.style.removeProperty("cursor");
    document.body.style.removeProperty("user-select");
    setListWidth(liveWidthRef.current);
  }, [setListWidth]);

  // A drag interrupted by an unmount would otherwise leave the whole app with
  // `user-select: none`.
  React.useEffect(() => () => {
    if (!dragRef.current) return;
    document.body.style.removeProperty("cursor");
    document.body.style.removeProperty("user-select");
  }, []);

  const startListResize = React.useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (e.button !== 0) return;
      const el = listRef.current;
      if (!el) return;
      const sep = e.currentTarget;
      // Deliberately no preventDefault: it would suppress the compatibility
      // mousedown, and with it both the focus this control needs for its
      // keyboard path and the mousedown every click-outside listener in the
      // app is registered on. The body user-select lock below is what stops
      // the drag selecting text.
      try {
        sep.setPointerCapture(e.pointerId);
      } catch {
        // jsdom, and any browser that has already lost the pointer.
      }
      sep.focus();
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      dragRef.current = {
        // The grab offset inside the 6px handle is part of the start width, so
        // the divider stays under the cursor instead of jumping to meet it.
        startX: e.clientX,
        startWidth: el.getBoundingClientRect().width || renderedWidth,
        max: measureMax(),
      };
      liveWidthRef.current = renderedWidth;
    },
    [measureMax, renderedWidth],
  );

  const onListResizeMove = React.useCallback((e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag) return;
    const next = Math.round(
      Math.min(
        drag.max,
        Math.max(UNIBOX_LIST_MIN_WIDTH, drag.startWidth + (e.clientX - drag.startX)),
      ),
    );
    if (next === liveWidthRef.current) return;
    liveWidthRef.current = next;
    // Straight to the DOM for the duration. Routing every pointer frame through
    // the store would re-render the whole inbox and, because the store is
    // persisted, serialise and write localStorage on each one.
    listRef.current?.style.setProperty("--unibox-list-w", `${next}px`);
  }, []);

  // The ARIA window-splitter keys: arrows nudge (shift for a coarse step),
  // Home/End go to the bounds, Enter restores the default, which is also what a
  // double-click does.
  const onListResizeKey = React.useCallback(
    (e: React.KeyboardEvent) => {
      const step = e.shiftKey ? 48 : 16;
      const current = useAppStore.getState().uniboxListWidth;
      switch (e.key) {
        case "ArrowLeft":
          setListWidth(current - step);
          break;
        case "ArrowRight":
          setListWidth(current + step);
          break;
        case "Home":
          setListWidth(UNIBOX_LIST_MIN_WIDTH);
          break;
        case "End":
          setListWidth(UNIBOX_LIST_MAX_WIDTH);
          break;
        case "Enter":
          setListWidth(UNIBOX_LIST_DEFAULT_WIDTH);
          break;
        default:
          return;
      }
      e.preventDefault();
    },
    [setListWidth],
  );

  // goTo writes the URL by merging the requested changes over the current path
  // (an omitted field keeps its current value; pass null to clear).
  const goTo = React.useCallback(
    (next: { scope?: string; threadId?: string | null; ref?: string | null }) => {
      const scope = next.scope ?? urlScope;
      const threadId =
        next.threadId === undefined ? urlThread : next.threadId;
      const ref = next.ref === undefined ? urlScopeRef : next.ref;
      let path = `/app/unibox/${scope || "all"}`;
      if (threadId) path += `/${encodeURIComponent(threadId)}`;
      if (ref) path += `?ref=${encodeURIComponent(ref)}`;
      navigate(path, { replace: true });
    },
    [navigate, urlScope, urlThread, urlScopeRef],
  );

  // Keep the open thread in sync between the URL (deep-linkable) and the store
  // (set by row clicks + keyboard nav) through a single reconciler. Tracking
  // which side actually moved lets the two writers converge; two mutually
  // writing effects would instead swap the values every commit, remounting the
  // thread pane in a tight loop whenever the store and URL start out disagreeing
  // (a stale singleton thread id carried into a fresh /app/unibox/all session).
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);
  const storeThread = useAppStore((s) => s.selectedThreadId);
  const lastUrlThread = React.useRef(urlThread);
  const lastStoreThread = React.useRef(storeThread);
  React.useEffect(() => {
    const urlMoved = urlThread !== lastUrlThread.current;
    const storeMoved = storeThread !== lastStoreThread.current;
    lastUrlThread.current = urlThread;
    lastStoreThread.current = storeThread;
    if (urlThread === storeThread) return;
    // The store wins only when it alone moved (a click / keypress); otherwise
    // the URL is the source of truth (deep link, back/forward, mount mismatch).
    if (storeMoved && !urlMoved) goTo({ threadId: storeThread });
    else setSelectedThreadId(urlThread);
  }, [urlThread, storeThread, setSelectedThreadId, goTo]);

  // ── Scope (derived from URL + ref) ─────────────────────────────
  const scope: UniboxScope = React.useMemo(() => {
    switch (urlScope) {
      case "unread":
        return { kind: "unread" };
      case "today":
        return { kind: "today" };
      case "week":
        return { kind: "week" };
      case "awaiting":
        return { kind: "awaiting" };
      case "agent_drafts":
        return { kind: "agent_drafts" };
      case "snoozed":
        return { kind: "snoozed" };
      case "scheduled":
        return { kind: "scheduled" };
      case "inbox":
      case "sent":
      case "drafts":
      case "archive":
      case "spam":
      case "trash":
        // Folder scopes are direct URL segments: /app/unibox/spam.
        return { kind: "folder", folder: urlScope };
      case "mailbox":
        return urlScopeRef
          ? { kind: "mailbox", mailboxId: urlScopeRef }
          : { kind: "all" };
      case "tag":
        return urlScopeRef
          ? { kind: "tag", tagId: urlScopeRef }
          : { kind: "all" };
      case "category":
        return urlScopeRef
          ? { kind: "category", categoryId: urlScopeRef }
          : { kind: "all" };
      default:
        return { kind: "all" };
    }
  }, [urlScope, urlScopeRef]);

  const setScope = React.useCallback(
    (s: UniboxScope) => {
      switch (s.kind) {
        case "folder":
          goTo({ scope: s.folder, ref: null });
          return;
        case "mailbox":
          goTo({ scope: "mailbox", ref: s.mailboxId });
          return;
        case "tag":
          goTo({ scope: "tag", ref: s.tagId });
          return;
        case "category":
          goTo({ scope: "category", ref: s.categoryId });
          return;
        case "all":
          goTo({ scope: "all", ref: null });
          return;
        default:
          goTo({ scope: s.kind, ref: null });
      }
    },
    [goTo],
  );

  // ── Scope → server search params ───────────────────────────────
  // Derived synchronously (initial state + render-phase reset), NOT in
  // an effect: an effect runs after paint, so on a reload of a scoped
  // URL the list would fire and render the default "all" query first,
  // then flash to the scoped one.
  const storeEmails = useAppStore((s) => s.emails);
  const tagAccountIds = React.useMemo(
    () =>
      scope.kind === "tag"
        ? storeEmails
            .filter((m) => (m.tags ?? []).includes(scope.tagId))
            .map((m) => m.id)
        : null,
    [scope, storeEmails],
  );
  const paramsForScope = React.useCallback(
    (sortBy: UniboxSearchParams["sortBy"]): UniboxSearchParams => {
      const next: UniboxSearchParams = { sortBy: sortBy ?? "newest" };
      switch (scope.kind) {
        case "unread":
          next.unseen = true;
          break;
        case "today":
          next.since = startOfToday();
          break;
        case "week":
          next.since = startOfWeek();
          break;
        case "awaiting":
          next.awaitingReply = true;
          break;
        case "agent_drafts":
          next.agentDrafts = true;
          break;
        case "snoozed":
          next.snoozed = true;
          break;
        case "folder":
          next.folder = scope.folder;
          break;
        case "mailbox":
          next.accountIds = [scope.mailboxId];
          break;
        case "tag":
          // Tag→mailbox membership resolves through the store's
          // mailbox directory (populated by DataSyncProvider); the
          // server only knows accountIds.
          next.accountIds = tagAccountIds ?? [];
          next.tagId = scope.tagId;
          break;
        case "category":
          // Conversation-label scope resolves to a server-side
          // category filter (category_ids); no client resolution
          // needed since labels live on the thread, not the
          // mailbox.
          next.categoryIds = [scope.categoryId];
          break;
        default:
          break;
      }
      return next;
    },
    [scope, tagAccountIds],
  );
  const [params, setParams] = React.useState<UniboxSearchParams>(() =>
    paramsForScope("newest"),
  );
  // Reset filters when the scope changes (or a tag scope re-resolves as
  // the mailbox directory loads), keeping only the sort. Setting state
  // during render re-renders before commit, so the stale params never
  // reach the query.
  const tagIdsKey = tagAccountIds?.join(",") ?? "";
  const [prevReset, setPrevReset] = React.useState({ scope, tagIdsKey });
  if (prevReset.scope !== scope || prevReset.tagIdsKey !== tagIdsKey) {
    setPrevReset({ scope, tagIdsKey });
    setParams((prev) => paramsForScope(prev.sortBy));
  }

  // ── Scope label for header chip ────────────────────────────────
  const overviewData = overview.data;
  const scopeLabel = React.useMemo(() => {
    switch (scope.kind) {
      case "unread":
        return "Unread";
      case "today":
        return "Today";
      case "week":
        return "This week";
      case "awaiting":
        return "Awaiting reply";
      case "agent_drafts":
        return "Agent drafts";
      case "snoozed":
        return "Snoozed";
      case "scheduled":
        return "Scheduled";
      case "folder":
        return scope.folder.charAt(0).toUpperCase() + scope.folder.slice(1);
      case "mailbox": {
        const m = overviewData?.mailboxes.find((x) => x.id === scope.mailboxId);
        return m ? m.email : "Mailbox";
      }
      case "tag": {
        const t = overviewData?.tags.find((x) => x.id === scope.tagId);
        return t ? `Tag · ${t.title}` : "Tag";
      }
      case "category": {
        const c = overviewData?.categories?.find(
          (x) => x.id === scope.categoryId,
        );
        return c ? `Label · ${c.title}` : "Label";
      }
      default:
        return "All";
    }
  }, [scope, overviewData]);

  if (!canAccess) {
    return <NoAccess feature="the unified inbox" permissionLabel="Use unified inbox" />;
  }

  return (
    <LockedSurface
      locked={!access.loading && !access.hasInbox}
      feature="Unified inbox"
      blurb="Read and reply to every inbound message across every connected mailbox from one place — searchable, filterable, with realtime updates."
      minPlan="starter"
      bullets={[
        "Live overview: unread, awaiting reply, snoozed, today, week",
        "Scope rail with per-mailbox + per-tag unread counts",
        "Deep-linkable threads as a clean URL path",
        "Snooze any thread to clear it from the inbox until later",
      ]}
    >
      <div className="flex flex-col h-full bg-white">
        <UniboxHeader
          scopeLabel={scopeLabel}
          onClearScope={() => setScope({ kind: "all" })}
          onOpenScopeSheet={() => setScopeSheetOpen(true)}
        />

        <ScopeSheet
          open={scopeSheetOpen}
          setOpen={setScopeSheetOpen}
          scope={scope}
          onChange={setScope}
        />

        <div ref={rowRef} className="flex-1 min-h-0 flex">
          <aside className="hidden lg:flex w-[220px] shrink-0 h-full">
            <ScopeRail scope={scope} onChange={setScope} />
          </aside>

          {scope.kind === "scheduled" ? (
            // Scheduled scope takes the full right side — a
            // queued send has no thread context to load.
            <div className="flex-1 min-w-0 flex flex-col overflow-hidden border-l border-slate-200">
              <ScheduledList />
            </div>
          ) : (
            <>
              <div
                ref={listRef}
                id="unibox-conversation-list"
                // The width only applies from md up; below it the list is the
                // whole screen and the thread replaces it. Already measured
                // against the viewport, so no CSS cap is needed on top.
                style={{ "--unibox-list-w": `${renderedWidth}px` } as React.CSSProperties}
                className={cn(
                  "w-full shrink-0 overflow-hidden flex-col md:w-[var(--unibox-list-w)]",
                  urlThread ? "hidden md:flex" : "flex",
                )}
              >
                <ConversationList
                  scopeKey={scopeKey(scope)}
                  scopeLabel={scopeLabel}
                  params={params}
                  setParams={setParams}
                />
              </div>

              {/* The divider IS the drag handle: a 6px column with the
                  hairline centred in it, so the grab area never overlaps
                  either pane's scrollbar. */}
              <div
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize the conversation list"
                aria-controls="unibox-conversation-list"
                aria-valuenow={renderedWidth}
                aria-valuemin={UNIBOX_LIST_MIN_WIDTH}
                aria-valuemax={Math.min(UNIBOX_LIST_MAX_WIDTH, maxWidth)}
                aria-valuetext={`Conversation list ${renderedWidth} pixels`}
                tabIndex={0}
                onPointerDown={startListResize}
                onPointerMove={onListResizeMove}
                onPointerUp={endDrag}
                onPointerCancel={endDrag}
                onLostPointerCapture={endDrag}
                onKeyDown={onListResizeKey}
                onDoubleClick={() => setListWidth(UNIBOX_LIST_DEFAULT_WIDTH)}
                className="group hidden md:flex w-1.5 shrink-0 cursor-col-resize items-stretch justify-center touch-none outline-none"
              >
                {/* The hairline is the whole control, so focus has to thicken
                    and colour it: there is no outline to fall back on. */}
                <span className="w-px bg-slate-200 transition-[background-color,width] group-hover:bg-sky-400 group-active:bg-sky-500 group-focus-visible:w-0.5 group-focus-visible:bg-sky-500" />
              </div>

              <div
                className={cn(
                  "flex-1 min-w-0 overflow-hidden flex-col",
                  urlThread ? "flex" : "hidden md:flex",
                )}
              >
                {urlThread ? (
                  <>
                    <button
                      type="button"
                      onClick={() => goTo({ threadId: null })}
                      className="md:hidden flex items-center gap-1 px-3 h-10 shrink-0 border-b border-slate-200 text-[13px] font-medium text-slate-600 hover:text-slate-900 active:bg-slate-50"
                    >
                      <ChevronLeftIcon className="w-4 h-4" />
                      Inbox
                    </button>
                    <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
                      {/* Keyed: the list is what has to survive a thread
                          change, the reader is what has to start clean, so a
                          half-typed reply never follows you to the next
                          conversation. */}
                      <ThreadView key={urlThread} threadId={urlThread} />
                    </div>
                  </>
                ) : (
                  <div className="flex-1 flex items-center justify-center">
                    <div className="text-center px-5">
                      <div className="w-8 h-8 rounded-md bg-slate-100 flex items-center justify-center mx-auto mb-3 text-slate-400">
                        <InboxIcon className="w-4 h-4" />
                      </div>
                      <p className="text-[12.5px] font-medium text-slate-700">
                        Select a conversation
                      </p>
                      <p className="text-[11.5px] text-slate-400 mt-1 max-w-[34ch] leading-relaxed">
                        Pick a thread from the list. It opens in the URL path so
                        you can share or refresh.
                      </p>
                    </div>
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </LockedSurface>
  );
}
