// Unibox — three-column overview layout.
//
//   ┌── Top metric strip ─────────────────────────────────────────┐
//   │ Inbox · [scope chip] · unread · awaiting · today · week · …│
//   ├──────────┬────────────────────────┬─────────────────────────┤
//   │  Scope   │ Conversation list      │ Thread (live fetch)     │
//   │  rail    │ (search + dense rows)  │ (deep-linkable URL)     │
//   │ (220px)  │       (360px)          │  flex-1                 │
//   └──────────┴────────────────────────┴─────────────────────────┘
//
// All counts in the rail and strip come from /unibox/overview in one
// round trip — server truth, no client guesswork. Snoozed and
// Awaiting reply are real backend scopes, not "soon" placeholders.

import React from "react";
import { useSearchParams } from "react-router-dom";
import { ChevronLeftIcon, InboxIcon } from "lucide-react";

import { ConversationList } from "@/components/app/unibox/ConversationList";
import { ScheduledList } from "@/components/app/unibox/ScheduledList";
import { ThreadView } from "@/components/app/unibox/ThreadView";
import { ScopeRail, type UniboxScope } from "@/components/app/unibox/ScopeRail";
import { ScopeSheet } from "@/components/app/unibox/ScopeSheet";
import { UniboxHeader } from "@/components/app/unibox/UniboxHeader";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { LockedSurface } from "@/components/layout/LockedSurface";
import { useAppStore } from "@/stores";
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
  const overview = useUniboxOverview();
  const [searchParams, setSearchParams] = useSearchParams();
  const [scopeSheetOpen, setScopeSheetOpen] = React.useState(false);

  // ── URL state (deep-linkable threads + scope) ──────────────────
  const urlThread = searchParams.get("thread");
  const urlAccount = searchParams.get("account");
  const urlScope = searchParams.get("scope") ?? "all";
  const urlScopeRef = searchParams.get("ref");

  const setUrl = React.useCallback(
    (next: {
      thread?: string | null;
      account?: string | null;
      scope?: string | null;
      ref?: string | null;
    }) => {
      setSearchParams(
        (prev) => {
          const sp = new URLSearchParams(prev);
          for (const [k, v] of Object.entries(next)) {
            if (v === null || v === undefined || v === "") sp.delete(k);
            else sp.set(k, v);
          }
          return sp;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Mirror URL ↔ store so ConversationItem clicks update the URL.
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);
  const setSelectedAccountId = useAppStore((s) => s.setSelectedAccountId);
  React.useEffect(() => {
    setSelectedThreadId(urlThread);
    setSelectedAccountId(urlAccount);
  }, [urlThread, urlAccount, setSelectedThreadId, setSelectedAccountId]);

  const storeThread = useAppStore((s) => s.selectedThreadId);
  const storeAccount = useAppStore((s) => s.selectedAccountId);
  React.useEffect(() => {
    if (storeThread !== urlThread || storeAccount !== urlAccount) {
      setUrl({ thread: storeThread, account: storeAccount });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storeThread, storeAccount]);

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
      case "snoozed":
        return { kind: "snoozed" };
      case "scheduled":
        return { kind: "scheduled" };
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
        case "mailbox":
          setUrl({ scope: "mailbox", ref: s.mailboxId });
          return;
        case "tag":
          setUrl({ scope: "tag", ref: s.tagId });
          return;
        case "category":
          setUrl({ scope: "category", ref: s.categoryId });
          return;
        case "all":
          setUrl({ scope: null, ref: null });
          return;
        default:
          setUrl({ scope: s.kind, ref: null });
      }
    },
    [setUrl],
  );

  // ── Scope → server search params ───────────────────────────────
  // Mailboxes/tags come from the same overview payload so we don't
  // race a separate /emails fetch when resolving a tag.
  const [params, setParams] = React.useState<UniboxSearchParams>({
    sortBy: "newest",
  });
  const overviewData = overview.data;
  React.useEffect(() => {
    setParams((prev) => {
      const next: UniboxSearchParams = { sortBy: prev.sortBy ?? "newest" };
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
        case "snoozed":
          next.snoozed = true;
          break;
        case "mailbox":
          next.accountIds = [scope.mailboxId];
          break;
        case "tag": {
          // Tag-scoped account resolution: overview already lists
          // the user's mailboxes, but tag→mailbox membership is
          // not in the overview payload. We fall back to the
          // dataSlice's emails, which the existing user-profile
          // bootstrap populates.
          const ids = useAppStore
            .getState()
            .emails.filter((m) => (m.tags ?? []).includes(scope.tagId))
            .map((m) => m.id);
          next.accountIds = ids;
          next.tagId = scope.tagId;
          break;
        }
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
    });
  }, [scope, overviewData]);

  // ── Scope label for header chip ────────────────────────────────
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
      case "snoozed":
        return "Snoozed";
      case "scheduled":
        return "Scheduled";
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

  return (
    <LockedSurface
      locked={!access.loading && !access.hasInbox}
      feature="Unified inbox"
      blurb="Read and reply to every inbound message across every connected mailbox from one place — searchable, filterable, with realtime updates."
      minPlan="starter"
      bullets={[
        "Live overview: unread, awaiting reply, snoozed, today, week",
        "Scope rail with per-mailbox + per-tag unread counts",
        "Deep-linkable threads via ?thread= in the URL",
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

        <div className="flex-1 min-h-0 flex">
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
                className={cn(
                  "w-full md:w-[360px] shrink-0 border-r border-slate-200 overflow-hidden flex-col",
                  urlThread ? "hidden md:flex" : "flex",
                )}
              >
                <ConversationList
                  scopeLabel={scopeLabel}
                  params={params}
                  setParams={setParams}
                />
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
                      onClick={() => setUrl({ thread: null, account: null })}
                      className="md:hidden flex items-center gap-1 px-3 h-10 shrink-0 border-b border-slate-200 text-[13px] font-medium text-slate-600 hover:text-slate-900 active:bg-slate-50"
                    >
                      <ChevronLeftIcon className="w-4 h-4" />
                      Inbox
                    </button>
                    <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
                      <ThreadView
                        threadId={urlThread}
                        emailId={urlAccount ?? undefined}
                      />
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
                        Pick a thread from the list — its id will appear in the
                        URL so you can share or refresh.
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
