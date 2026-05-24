// Conversation list — middle pane of the unibox.
//
// Now fully wired to the backend search endpoint via useUniboxSearch.
// The page builds a UniboxSearchParams object out of:
//
//   - a quick-filter strip at the top (All / Unread / Today / This week)
//   - a free-text search input (subject substring server-side)
//   - an advanced filter sheet (sender, account, status, dates, sort)
//
// Realtime: when a new email arrives, the WS slice adds it to the
// store; the list still renders from server data but shows a sticky
// "N new" pill at the top for fresh entries.

import React from "react";
import {
    Loader2Icon,
    Settings2Icon,
} from "lucide-react";
import { SearchInput } from "@/components/ui/field";
import { ConversationItem } from "./ConversationItem";
import { SectionBar } from "@/components/layout/Page";
import useUniboxSearch from "@/lib/api/hooks/app/unibox/useUniboxSearch";
import useUnseenCount from "@/lib/api/hooks/app/unibox/useUnseenCount";
import { UniboxFilterSheet } from "./UniboxFilterSheet";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

type Quick = "all" | "unread" | "today" | "week";

const QUICKS: { id: Quick; label: string }[] = [
    { id: "all", label: "All" },
    { id: "unread", label: "Unread" },
    { id: "today", label: "Today" },
    { id: "week", label: "This week" },
];

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

export function ConversationList() {
    const [search, setSearch] = React.useState("");
    const [quick, setQuick] = React.useState<Quick>("all");
    const [advanced, setAdvanced] = React.useState<UniboxSearchParams>({ sortBy: "newest" });
    const [sheetOpen, setSheetOpen] = React.useState(false);

    // Combine quick filter + free-text search + advanced filters into
    // one params object react-query keys on.
    const params: UniboxSearchParams = React.useMemo(() => {
        const next: UniboxSearchParams = { ...advanced };
        if (search.trim()) next.query = search.trim();
        if (quick === "unread") next.unseen = true;
        else if (quick === "today") next.since = startOfToday();
        else if (quick === "week") next.since = startOfWeek();
        return next;
    }, [advanced, search, quick]);

    const q = useUniboxSearch(params);
    const unseen = useUnseenCount();

    const emails = q.emails;
    const totalShown = emails.length;
    const unseenCount = unseen.data?.count ?? 0;

    return (
        <div className="flex flex-col h-full">
            <SectionBar label="Inbox" count={unseenCount > 0 ? unseenCount : 0}>
                <button
                    type="button"
                    onClick={() => setSheetOpen(true)}
                    className="size-6 rounded text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    aria-label="Advanced filters"
                >
                    <Settings2Icon className="w-3.5 h-3.5" />
                </button>
            </SectionBar>

            <div className="px-3 py-2 shrink-0 border-b border-slate-200 space-y-2">
                <SearchInput
                    value={search}
                    onChange={setSearch}
                    placeholder="Search subject…"
                />
                <div className="flex items-center gap-0.5 overflow-x-auto">
                    {QUICKS.map((f) => (
                        <button
                            key={f.id}
                            onClick={() => setQuick(f.id)}
                            className={`shrink-0 h-6 px-2 rounded text-[11.5px] font-medium transition-colors inline-flex items-center gap-1 ${
                                quick === f.id
                                    ? "bg-slate-900 text-white"
                                    : "text-slate-500 hover:text-slate-900 hover:bg-slate-100"
                            }`}
                        >
                            {f.label}
                            {f.id === "unread" && unseenCount > 0 && (
                                <span
                                    className={`font-mono tabular-nums text-[10px] ${
                                        quick === f.id ? "text-white/80" : "text-sky-600"
                                    }`}
                                >
                                    {unseenCount}
                                </span>
                            )}
                        </button>
                    ))}
                </div>
            </div>

            <div className="flex-1 overflow-y-auto">
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
                            {search.trim() || params.from || params.accountId || params.unseen || params.since
                                ? "No matches"
                                : "Nothing in your inbox yet"}
                        </p>
                        <p className="text-[11.5px] text-slate-400 max-w-[28ch] mx-auto leading-relaxed">
                            {search.trim() || params.from || params.accountId || params.unseen || params.since
                                ? "Try a different filter or clear them all."
                                : "Replies and inbound mail will land here automatically."}
                        </p>
                    </div>
                ) : (
                    <>
                        <div className="divide-y divide-slate-200/60">
                            {emails.map((e) => (
                                <ConversationItem
                                    key={e.id}
                                    email={{
                                        id: e.id,
                                        from: "", // server doesn't include from_addr in preview yet
                                        to: "",
                                        subject: e.subject,
                                        body: e.snippet,
                                        date: new Date(e.internal_date),
                                        is_seen: e.seen,
                                        thread_id: e.thread_id,
                                        account_id: e.email_id,
                                    }}
                                />
                            ))}
                        </div>
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

            <UniboxFilterSheet
                open={sheetOpen}
                setOpen={setSheetOpen}
                filters={advanced}
                setFilters={setAdvanced}
                loading={q.isFetching}
            />
        </div>
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
