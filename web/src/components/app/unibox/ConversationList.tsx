// Conversation list — middle pane of the unibox.
//
// Rows show from / subject / preview / time. Unread rows get a small
// sky rail on the left margin and a bolder from-name.

import { useState } from "react";
import { useAppStore } from "@/stores";
import { SearchInput } from "@/components/ui/field";
import { ConversationItem } from "./ConversationItem";
import { SectionBar } from "@/components/layout/Page";

type Filter = "all" | "unread";

const FILTERS: Array<{ id: Filter; label: string }> = [
    { id: "all", label: "All" },
    { id: "unread", label: "Unread" },
];

export function ConversationList() {
    const [search, setSearch] = useState("");
    const [filter, setFilter] = useState<Filter>("all");
    const emails = useAppStore((s) => s.uniboxEmails);

    const filtered = emails.filter((email) => {
        if (filter === "unread" && email.is_seen) return false;
        if (search) {
            const q = search.toLowerCase();
            return (
                email.subject.toLowerCase().includes(q) ||
                email.from.toLowerCase().includes(q)
            );
        }
        return true;
    });

    const unreadCount = emails.filter((e) => !e.is_seen).length;

    return (
        <div className="flex flex-col h-full">
            <SectionBar label="Inbox" count={filtered.length} />
            <div className="px-3 py-2 shrink-0 border-b border-slate-200">
                <SearchInput
                    value={search}
                    onChange={setSearch}
                    placeholder="Search conversations…"
                />
                <div className="flex items-center gap-0.5 mt-2">
                    {FILTERS.map((f) => (
                        <button
                            key={f.id}
                            onClick={() => setFilter(f.id)}
                            className={`h-6 px-2 rounded text-[11.5px] font-medium transition-colors inline-flex items-center gap-1 ${
                                filter === f.id
                                    ? "bg-slate-900 text-white"
                                    : "text-slate-500 hover:text-slate-900 hover:bg-slate-100"
                            }`}
                        >
                            {f.label}
                            {f.id === "unread" && unreadCount > 0 && (
                                <span
                                    className={`font-mono tabular-nums text-[10px] ${
                                        filter === f.id ? "text-white/80" : "text-sky-600"
                                    }`}
                                >
                                    {unreadCount}
                                </span>
                            )}
                        </button>
                    ))}
                </div>
            </div>

            <div className="flex-1 overflow-y-auto">
                {filtered.length === 0 ? (
                    <div className="px-5 py-16 text-center">
                        <p className="text-[12.5px] text-slate-700 font-medium mb-1">
                            {search
                                ? "No matches"
                                : filter === "unread"
                                    ? "All caught up"
                                    : "No conversations yet"}
                        </p>
                        <p className="text-[11.5px] text-slate-400 max-w-[28ch] mx-auto leading-relaxed">
                            {search
                                ? "Try a different keyword."
                                : filter === "unread"
                                    ? "When new mail arrives it'll show up here."
                                    : "Replies and inbound mail land here automatically."}
                        </p>
                    </div>
                ) : (
                    <div className="divide-y divide-slate-200/60">
                        {filtered.map((email) => (
                            <ConversationItem key={email.id} email={email} />
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}
