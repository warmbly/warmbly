// The unibox's LinkedIn scope: the workspace's LinkedIn conversations, latest activity first.
import React from "react";
import { Link } from "@tanstack/react-router";
import { Loader2Icon, PanelLeftIcon } from "lucide-react";
import { useLinkedInConversations } from "@/lib/api/hooks/app/linkedin/useLinkedIn";
import type { LinkedInConversation } from "@/lib/api/models/app/linkedin/LinkedIn";
import { useResourceViewers } from "@/hooks/PresenceProvider";
import { Avatar } from "@/components/app/linkedin/parts";
import { linkedInErrorMessage } from "@/lib/linkedin/errors";
import { cn } from "@/lib/utils";

function relativeTime(v: Date | string | null | undefined): string {
    if (!v) return "";
    const d = new Date(v);
    const m = Math.floor((Date.now() - d.getTime()) / 60_000);
    if (m < 1) return "now";
    if (m < 60) return `${m}m`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h`;
    const days = Math.floor(h / 24);
    if (days < 7) return `${days}d`;
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export function LinkedInConversationList({
    selectedId,
    onOpen,
    onOpenScopeSheet,
}: {
    selectedId: string | null;
    onOpen: (id: string) => void;
    onOpenScopeSheet?: () => void;
}) {
    const [unreadOnly, setUnreadOnly] = React.useState(false);
    const q = useLinkedInConversations({ unread: unreadOnly });
    const rows = React.useMemo(() => q.data?.pages.flatMap((p) => p.data) ?? [], [q.data]);

    // The next page loads as the end of the list scrolls into view.
    const sentinel = React.useRef<HTMLDivElement>(null);
    const { hasNextPage, isFetchingNextPage, fetchNextPage } = q;
    React.useEffect(() => {
        const el = sentinel.current;
        if (!el || !hasNextPage) return;
        const io = new IntersectionObserver((entries) => {
            if (entries[0]?.isIntersecting && !isFetchingNextPage) void fetchNextPage();
        });
        io.observe(el);
        return () => io.disconnect();
    }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

    return (
        <div className="relative flex h-full flex-col bg-white">
            <div className="h-11 shrink-0 pl-3 pr-2 flex items-center gap-1.5">
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
                <h1 className="min-w-0 truncate text-[13.5px] font-semibold text-slate-900">LinkedIn</h1>
                {rows.length > 0 && (
                    <span className="shrink-0 tabular-nums text-[11.5px] text-slate-400">
                        {rows.length}
                        {hasNextPage ? "+" : ""}
                    </span>
                )}
                <span className="flex-1" />
                <div className="inline-flex shrink-0 rounded-md bg-slate-100 p-0.5">
                    {([false, true] as const).map((u) => (
                        <button
                            key={String(u)}
                            type="button"
                            onClick={() => setUnreadOnly(u)}
                            className={cn(
                                "h-6 rounded px-2 text-[11px] font-medium transition-colors",
                                unreadOnly === u ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-700",
                            )}
                        >
                            {u ? "Unread" : "All"}
                        </button>
                    ))}
                </div>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto border-t border-slate-200/70">
                {q.isPending ? (
                    <div className="divide-y divide-slate-100">
                        {[...Array(6)].map((_, i) => (
                            <div key={i} className="flex items-center gap-2.5 px-3 py-2.5">
                                <div className="size-8 rounded-full bg-slate-100 animate-pulse" />
                                <div className="flex-1 space-y-1.5">
                                    <div className="h-3 w-1/3 rounded bg-slate-100 animate-pulse" />
                                    <div className="h-3 w-2/3 rounded bg-slate-100 animate-pulse" />
                                </div>
                            </div>
                        ))}
                    </div>
                ) : q.isError ? (
                    <div className="px-5 py-10 text-center">
                        <p className="text-[12.5px] font-medium text-slate-700">The conversations could not be loaded</p>
                        <p className="mt-1 text-[11.5px] text-slate-500">{linkedInErrorMessage(q.error)}</p>
                        <button type="button" onClick={() => void q.refetch()} className="mt-3 h-7 rounded-md border border-slate-200 px-3 text-[12px] font-medium text-slate-700 hover:bg-slate-50">
                            Try again
                        </button>
                    </div>
                ) : rows.length === 0 ? (
                    <div className="px-5 py-10 text-center">
                        <p className="text-[12.5px] font-medium text-slate-700">{unreadOnly ? "Nothing unread" : "No LinkedIn conversations yet"}</p>
                        <p className="mx-auto mt-1 max-w-[36ch] text-[11.5px] leading-relaxed text-slate-500">
                            {unreadOnly ? "Every LinkedIn message has been read." : "Messages to and from your connected LinkedIn accounts show up here."}
                        </p>
                        {!unreadOnly && (
                            <Link to="/app/linkedin" className="mt-3 inline-flex h-7 items-center rounded-md border border-slate-200 px-3 text-[12px] font-medium text-slate-700 hover:bg-slate-50">
                                LinkedIn accounts
                            </Link>
                        )}
                    </div>
                ) : (
                    <ul className="divide-y divide-slate-100">
                        {rows.map((c) => (
                            <li key={c.id}>
                                <ConversationRow conversation={c} selected={c.id === selectedId} onOpen={() => onOpen(c.id)} />
                            </li>
                        ))}
                    </ul>
                )}
                <div ref={sentinel} className="h-px" />
                {isFetchingNextPage && (
                    <div className="flex justify-center py-3">
                        <Loader2Icon className="size-4 animate-spin text-slate-300" />
                    </div>
                )}
            </div>
        </div>
    );
}

function ConversationRow({ conversation: c, selected, onOpen }: { conversation: LinkedInConversation; selected: boolean; onOpen: () => void }) {
    const unread = c.unread_count > 0;
    const viewers = useResourceViewers(`linkedin:${c.id}`);
    const replier = viewers.find((v) => v.action === "replying");
    const name = c.participant.full_name || "LinkedIn member";
    return (
        <button
            type="button"
            onClick={onOpen}
            aria-current={selected ? "true" : undefined}
            className={cn("flex w-full items-start gap-2.5 px-3 py-2.5 text-left transition-colors", selected ? "bg-sky-50" : "hover:bg-slate-50")}
        >
            <span className="relative shrink-0">
                <Avatar src={c.participant.picture_url} name={name} size={32} />
                {unread && <span aria-hidden className="absolute -top-0.5 -right-0.5 size-2.5 rounded-full border-2 border-white bg-sky-500" />}
            </span>
            <span className="min-w-0 flex-1">
                <span className="flex h-[18px] min-w-0 items-center gap-2">
                    <span className={cn("min-w-0 truncate text-[12.5px]", unread ? "font-semibold text-slate-900" : "text-slate-700")}>{name}</span>
                    {c.kind === "inmail" && <span className="shrink-0 rounded bg-amber-50 px-1 text-[10px] font-medium text-amber-700 ring-1 ring-amber-200">InMail</span>}
                    {viewers.length > 0 && (
                        <span className="relative flex size-2 shrink-0" title={replier ? `${replier.name ?? "A teammate"} is replying` : `${viewers[0].name ?? "A teammate"} is viewing`}>
                            <span className={cn("absolute inline-flex h-full w-full animate-ping rounded-full opacity-60", replier ? "bg-amber-400" : "bg-emerald-400")} />
                            <span className={cn("relative inline-flex size-2 rounded-full", replier ? "bg-amber-500" : "bg-emerald-500")} />
                        </span>
                    )}
                    <span className={cn("ml-auto shrink-0 text-[11px] tabular-nums", unread ? "font-medium text-sky-700" : "text-slate-400")}>{relativeTime(c.last_message_at ?? c.updated_at)}</span>
                </span>
                {c.participant.headline && <span className="block truncate text-[11.5px] text-slate-400">{c.participant.headline}</span>}
                <span className={cn("mt-0.5 block truncate text-[12px]", unread ? "font-medium text-slate-800" : "text-slate-500")}>{c.last_message_preview || "No messages yet"}</span>
            </span>
        </button>
    );
}
