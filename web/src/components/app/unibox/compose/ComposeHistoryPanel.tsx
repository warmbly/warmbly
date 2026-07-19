// ComposeHistoryPanel — everything you've ever exchanged with the recipient,
// without leaving the composer. Tabs split the full back-and-forth from just
// what we sent them; the search box narrows by subject. Rows open the real
// thread in the unibox so the composer stays a quick reference, not a fork
// of the inbox.

import React from "react";
import { useNavigate } from "react-router-dom";
import {
    ArrowUpRightIcon,
    HistoryIcon,
    Loader2Icon,
    SendIcon,
} from "lucide-react";
import useUniboxSearch from "@/lib/api/hooks/app/unibox/useUniboxSearch";
import type { UniboxListRow } from "@/lib/api/client/app/unibox/searchIncoming";
import { SearchInput } from "@/components/ui/field";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";

type HistoryTab = "all" | "sent";

interface ComposeHistoryPanelProps {
    // Bare recipient address the history is scoped to.
    address: string;
    // Display name when the recipient resolved to a contact.
    displayName?: string;
    // Mailbox affinity line, e.g. "usually from alex@acme.com".
    affinityLine?: string;
}

function bareEmail(s: string): string {
    const m = s.match(/<([^>]+)>/);
    if (m) return m[1].trim();
    return s.trim();
}

function formatWhen(iso: string): string {
    const d = new Date(iso);
    const now = new Date();
    const sameYear = d.getFullYear() === now.getFullYear();
    const sameDay =
        sameYear && d.getMonth() === now.getMonth() && d.getDate() === now.getDate();
    if (sameDay) return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
    return d.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        ...(sameYear ? {} : { year: "2-digit" }),
    });
}

export default function ComposeHistoryPanel({
    address,
    displayName,
    affinityLine,
}: ComposeHistoryPanelProps) {
    const navigate = useNavigate();
    const accounts = useAppStore((s) => s.emails);
    const [tab, setTab] = React.useState<HistoryTab>("all");
    const [search, setSearch] = React.useState("");

    const q = useUniboxSearch(
        {
            address,
            direction: tab === "sent" ? "sent" : undefined,
            query: search.trim() || undefined,
            // History is reference material: include snoozed threads too.
            snoozed: "any",
        },
        !!address,
    );

    const ourAddresses = React.useMemo(
        () => new Set(accounts.map((a) => a.email.toLowerCase())),
        [accounts],
    );

    const isOurs = React.useCallback(
        (row: UniboxListRow) =>
            (row.from_addr ?? []).some((f) => ourAddresses.has(bareEmail(f).toLowerCase())),
        [ourAddresses],
    );

    return (
        <div className="flex flex-col h-full min-h-0">
            <div className="shrink-0 px-3 pt-2.5 pb-2 border-b border-slate-100">
                <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold">
                    <HistoryIcon className="w-3 h-3" />
                    <span className="truncate">History with {displayName || address}</span>
                </div>
                {affinityLine && (
                    <div className="mt-1 text-[10.5px] text-emerald-700">{affinityLine}</div>
                )}
                <div className="mt-2 flex items-center gap-1">
                    {(["all", "sent"] as const).map((t) => (
                        <button
                            key={t}
                            type="button"
                            onClick={() => setTab(t)}
                            className={cn(
                                "h-6 px-2 rounded-md text-[11px] font-medium transition-colors",
                                tab === t
                                    ? "bg-slate-900 text-white"
                                    : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                            )}
                        >
                            {t === "all" ? "Conversations" : "Sent"}
                        </button>
                    ))}
                </div>
                <div className="mt-2">
                    <SearchInput
                        value={search}
                        onChange={setSearch}
                        placeholder="Search subjects…"
                    />
                </div>
            </div>

            <div className="flex-1 min-h-0 overflow-y-auto">
                {q.isPending && (
                    <div className="px-3 py-6 flex items-center justify-center text-slate-400">
                        <Loader2Icon className="w-4 h-4 animate-spin" />
                    </div>
                )}
                {!q.isPending && q.emails.length === 0 && (
                    <div className="px-3 py-6 text-center">
                        <p className="text-[12px] font-medium text-slate-600">
                            {tab === "sent" ? "Nothing sent yet" : "No conversations yet"}
                        </p>
                        <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">
                            {tab === "sent"
                                ? "Emails you send to this address will show up here."
                                : "This will be your first exchange with this address."}
                        </p>
                    </div>
                )}
                {q.emails.map((row) => {
                    const ours = isOurs(row);
                    return (
                        <button
                            key={row.id}
                            type="button"
                            onClick={() =>
                                navigate(`/app/unibox/all/${encodeURIComponent(row.thread_id || row.id)}`)
                            }
                            className="w-full px-3 py-2 flex items-start gap-2 text-left border-b border-slate-50 hover:bg-slate-50 transition-colors group"
                        >
                            <span
                                className={cn(
                                    "size-5 rounded-md inline-flex items-center justify-center shrink-0 mt-0.5",
                                    ours ? "bg-sky-50 text-sky-600" : "bg-slate-100 text-slate-400",
                                )}
                                title={ours ? "Latest message from you" : "Latest message from them"}
                            >
                                {ours ? (
                                    <SendIcon className="w-2.5 h-2.5" />
                                ) : (
                                    <HistoryIcon className="w-2.5 h-2.5" />
                                )}
                            </span>
                            <span className="min-w-0 flex-1">
                                <span className="flex items-baseline gap-1.5 min-w-0">
                                    <span
                                        className={cn(
                                            "text-[11.5px] truncate",
                                            row.has_unread
                                                ? "font-semibold text-slate-900"
                                                : "font-medium text-slate-700",
                                        )}
                                    >
                                        {row.subject || "(no subject)"}
                                    </span>
                                    {row.message_count > 1 && (
                                        <span className="font-mono text-[9.5px] text-slate-400 shrink-0">
                                            ×{row.message_count}
                                        </span>
                                    )}
                                    <span className="ml-auto font-mono text-[9.5px] text-slate-400 tabular-nums shrink-0">
                                        {formatWhen(row.internal_date)}
                                    </span>
                                </span>
                                {row.snippet && (
                                    <span className="block text-[10.5px] text-slate-400 truncate leading-snug">
                                        {row.snippet}
                                    </span>
                                )}
                            </span>
                            <ArrowUpRightIcon className="w-3 h-3 text-slate-300 opacity-0 group-hover:opacity-100 transition-opacity shrink-0 mt-1" />
                        </button>
                    );
                })}
                {q.hasNextPage && (
                    <button
                        type="button"
                        onClick={() => q.fetchNextPage()}
                        disabled={q.isFetchingNextPage}
                        className="w-full h-8 text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                    >
                        {q.isFetchingNextPage ? "Loading…" : "Load more"}
                    </button>
                )}
            </div>
        </div>
    );
}
