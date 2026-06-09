// Activity tab — the contact 360 timeline.
//
// Filtering pipeline (all client-side, applied to whatever pages are
// already in the cache):
//   - Type chips: All / Emails / Replies / Deliverability / Notes
//   - Free-text query against subject, content, campaign, sequence,
//     mailbox, reason, intent
//   - Date range [from, to] (inclusive day boundaries)
//
// Pagination is server-side cursor pagination (page size 50). The
// hook returns useInfiniteQuery state; this view auto-fetches the
// next page when the sentinel scrolls into view. When the user
// narrows the visible set with a filter, we lazy-prefetch a few more
// pages so the filtered list isn't artificially short.

import React from "react";
import { motion } from "framer-motion";
import {
    AlertOctagonIcon,
    BanIcon,
    CalendarIcon,
    CalendarClockIcon,
    CalendarPlusIcon,
    CalendarXIcon,
    Loader2Icon,
    MailIcon,
    MailOpenIcon,
    MailWarningIcon,
    MessageSquareIcon,
    MousePointerClickIcon,
    ReplyIcon,
    SearchIcon,
    StickyNoteIcon,
    XIcon,
} from "lucide-react";
import useContactTimeline from "@/lib/api/hooks/app/contacts/useContactTimeline";
import type ContactTimelineEvent from "@/lib/api/models/app/contacts/ContactTimelineEvent";
import type { ContactTimelineEventType } from "@/lib/api/models/app/contacts/ContactTimelineEvent";
import useClickOutside from "@/hooks/useClickOutside";
import { fmtAbsolute, fmtRelative } from "./format";

type FilterId = "all" | "emails" | "replies" | "deliv" | "notes" | "meetings";

const FILTERS: { id: FilterId; label: string }[] = [
    { id: "all", label: "All" },
    { id: "emails", label: "Emails" },
    { id: "replies", label: "Replies" },
    { id: "deliv", label: "Deliv." },
    { id: "notes", label: "Notes" },
    { id: "meetings", label: "Meetings" },
];

const EMAIL_TYPES: ContactTimelineEventType[] = [
    "email_sent",
    "email_opened",
    "email_clicked",
    "email_bounced",
];

const MEETING_TYPES: ContactTimelineEventType[] = [
    "meeting_booked",
    "meeting_rescheduled",
    "meeting_canceled",
];

export default function ActivityTab({ contactId }: { contactId: string }) {
    const {
        events,
        isLoading,
        isFetchingNextPage,
        error,
        hasNextPage,
        fetchNextPage,
    } = useContactTimeline(contactId);

    const [type, setType] = React.useState<FilterId>("all");
    const [query, setQuery] = React.useState("");
    const [from, setFrom] = React.useState("");
    const [to, setTo] = React.useState("");

    const visible = React.useMemo(
        () => applyFilters(events, { type, query, from, to }),
        [events, type, query, from, to],
    );

    // Auto-load: keep prefetching while filters are narrow and there's
    // more history. Bounded — we don't fetch forever; once 5 pages are
    // loaded, user has to scroll the sentinel.
    React.useEffect(() => {
        if (!hasNextPage || isFetchingNextPage) return;
        const filtered = type !== "all" || query !== "" || from !== "" || to !== "";
        if (filtered && visible.length < 15 && events.length < 250) {
            const t = setTimeout(() => fetchNextPage(), 120);
            return () => clearTimeout(t);
        }
    }, [
        visible.length,
        events.length,
        hasNextPage,
        isFetchingNextPage,
        type,
        query,
        from,
        to,
        fetchNextPage,
    ]);

    // Sentinel-driven infinite scroll. rootMargin pre-loads ~200px
    // before the bottom so the spinner barely flashes.
    const sentinelRef = React.useRef<HTMLDivElement>(null);
    React.useEffect(() => {
        const el = sentinelRef.current;
        if (!el || !hasNextPage) return;
        const io = new IntersectionObserver(
            (entries) => {
                if (entries[0]?.isIntersecting && !isFetchingNextPage) {
                    fetchNextPage();
                }
            },
            { rootMargin: "200px" },
        );
        io.observe(el);
        return () => io.disconnect();
    }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

    const anyFilter =
        type !== "all" || query !== "" || from !== "" || to !== "";

    function resetFilters() {
        setType("all");
        setQuery("");
        setFrom("");
        setTo("");
    }

    return (
        <div className="space-y-3">
            <SearchBar value={query} onChange={setQuery} />

            <div className="flex items-center gap-2 flex-wrap">
                <TypeChips value={type} onChange={setType} />
                <DateRange
                    from={from}
                    to={to}
                    setFrom={setFrom}
                    setTo={setTo}
                />
                {anyFilter && (
                    <button
                        type="button"
                        onClick={resetFilters}
                        className="h-6 px-2 rounded text-[11px] font-medium text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        Reset
                    </button>
                )}
                <span className="ml-auto text-[10.5px] text-slate-400 tabular-nums">
                    {isLoading
                        ? ""
                        : `${visible.length}${anyFilter ? ` of ${events.length}` : ""}`}
                </span>
            </div>

            {isLoading ? (
                <SkeletonList />
            ) : error ? (
                <div className="rounded-md border border-red-200 bg-red-50/60 px-3 py-2.5 text-[11.5px] text-red-700">
                    Failed to load activity.
                </div>
            ) : visible.length === 0 ? (
                <EmptyState anyFilter={anyFilter} onReset={resetFilters} />
            ) : (
                <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                    {visible.map((e, i) => (
                        <EventRow
                            key={`${e.type}-${e.at}-${i}`}
                            event={e}
                            highlight={query}
                        />
                    ))}
                </div>
            )}

            <div
                ref={sentinelRef}
                className="h-9 flex items-center justify-center"
            >
                {isFetchingNextPage ? (
                    <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-400">
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                        Loading more
                    </span>
                ) : !hasNextPage && events.length > 0 ? (
                    <span className="text-[10.5px] text-slate-300">
                        End of history
                    </span>
                ) : null}
            </div>
        </div>
    );
}

function applyFilters(
    events: ContactTimelineEvent[],
    f: { type: FilterId; query: string; from: string; to: string },
): ContactTimelineEvent[] {
    const ql = f.query.trim().toLowerCase();
    const fromMs = f.from ? new Date(f.from + "T00:00:00").getTime() : -Infinity;
    const toMs = f.to ? new Date(f.to + "T23:59:59.999").getTime() : Infinity;
    return events.filter((e) => {
        const tMs = new Date(e.at).getTime();
        if (tMs < fromMs || tMs > toMs) return false;
        switch (f.type) {
            case "emails":
                if (!EMAIL_TYPES.includes(e.type)) return false;
                break;
            case "replies":
                if (e.type !== "email_replied" && e.type !== "reply_received")
                    return false;
                break;
            case "deliv":
                if (e.type !== "deliverability" && e.type !== "suppressed")
                    return false;
                break;
            case "notes":
                if (e.type !== "note") return false;
                break;
            case "meetings":
                if (!MEETING_TYPES.includes(e.type)) return false;
                break;
            case "all":
                break;
        }
        if (ql) {
            const hay = [
                e.subject,
                e.content,
                e.campaign_name,
                e.sequence_name,
                e.email_account_email,
                e.email_account_name,
                e.reason,
                e.intent,
            ]
                .filter(Boolean)
                .join(" ")
                .toLowerCase();
            if (!hay.includes(ql)) return false;
        }
        return true;
    });
}

function SearchBar({
    value,
    onChange,
}: {
    value: string;
    onChange: (v: string) => void;
}) {
    return (
        <div className="relative">
            <SearchIcon className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none" />
            <input
                type="text"
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder="Search subject, content, campaign…"
                className="w-full h-8 pl-8 pr-7 rounded-md border border-slate-200 bg-white text-[12px] text-slate-900 placeholder:text-slate-400 focus:border-slate-400 outline-none transition-colors"
            />
            {value && (
                <button
                    type="button"
                    onClick={() => onChange("")}
                    aria-label="Clear search"
                    className="absolute right-1.5 top-1/2 -translate-y-1/2 size-5 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

function TypeChips({
    value,
    onChange,
}: {
    value: FilterId;
    onChange: (v: FilterId) => void;
}) {
    return (
        <div className="inline-flex bg-slate-100 rounded-md p-0.5">
            {FILTERS.map((f) => {
                const isActive = value === f.id;
                return (
                    <button
                        key={f.id}
                        type="button"
                        onClick={() => onChange(f.id)}
                        className="relative h-6 px-2 rounded text-[11px] font-medium outline-none whitespace-nowrap"
                    >
                        {isActive && (
                            <motion.div
                                layoutId="contact-activity-type"
                                className="absolute inset-0 rounded bg-white shadow-sm"
                                transition={{
                                    type: "spring",
                                    duration: 0.3,
                                    bounce: 0.15,
                                }}
                            />
                        )}
                        <span
                            className={`relative z-10 transition-colors ${
                                isActive
                                    ? "text-slate-900"
                                    : "text-slate-500 hover:text-slate-800"
                            }`}
                        >
                            {f.label}
                        </span>
                    </button>
                );
            })}
        </div>
    );
}

function DateRange({
    from,
    to,
    setFrom,
    setTo,
}: {
    from: string;
    to: string;
    setFrom: (v: string) => void;
    setTo: (v: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    const active = !!from || !!to;
    const label = active
        ? `${fmtChip(from) || "…"} → ${fmtChip(to) || "today"}`
        : "Any date";

    function setPreset(days: number) {
        const end = new Date();
        const start = new Date();
        start.setDate(start.getDate() - days);
        setFrom(toInput(start));
        setTo(toInput(end));
    }

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                className={`h-6 px-2 rounded text-[11px] font-medium inline-flex items-center gap-1 transition-colors ${
                    active
                        ? "bg-slate-900 text-white hover:bg-slate-800"
                        : "bg-slate-100 text-slate-600 hover:text-slate-900"
                }`}
            >
                <CalendarIcon className="w-3 h-3" />
                {label}
            </button>
            {open && (
                <div className="absolute right-0 top-7 z-50 w-64 max-w-[min(256px,calc(100vw-2rem))] p-2.5 rounded-md border border-slate-200 bg-white shadow-lg">
                    <div className="grid grid-cols-2 gap-2">
                        <div>
                            <label className="block text-[10px] uppercase tracking-[0.12em] font-medium text-slate-500 mb-1">
                                From
                            </label>
                            <input
                                type="date"
                                value={from}
                                onChange={(e) => setFrom(e.target.value)}
                                className="w-full h-7 px-1.5 rounded border border-slate-200 bg-white text-[11.5px] text-slate-900 outline-none focus:border-slate-400"
                            />
                        </div>
                        <div>
                            <label className="block text-[10px] uppercase tracking-[0.12em] font-medium text-slate-500 mb-1">
                                To
                            </label>
                            <input
                                type="date"
                                value={to}
                                onChange={(e) => setTo(e.target.value)}
                                className="w-full h-7 px-1.5 rounded border border-slate-200 bg-white text-[11.5px] text-slate-900 outline-none focus:border-slate-400"
                            />
                        </div>
                    </div>
                    <div className="flex flex-wrap gap-1 mt-2.5">
                        <Preset onClick={() => setPreset(7)}>Last 7d</Preset>
                        <Preset onClick={() => setPreset(30)}>Last 30d</Preset>
                        <Preset onClick={() => setPreset(90)}>Last 90d</Preset>
                        <button
                            type="button"
                            onClick={() => {
                                setFrom("");
                                setTo("");
                            }}
                            className="h-6 px-2 ml-auto rounded text-[10.5px] font-medium text-slate-500 hover:text-slate-900 hover:bg-slate-100"
                        >
                            Clear
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}

function Preset({
    onClick,
    children,
}: {
    onClick: () => void;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="h-6 px-2 rounded text-[10.5px] font-medium border border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-50 transition-colors"
        >
            {children}
        </button>
    );
}

function EmptyState({
    anyFilter,
    onReset,
}: {
    anyFilter: boolean;
    onReset: () => void;
}) {
    return (
        <div className="rounded-md border border-dashed border-slate-200 px-3 py-10 text-center">
            <div className="text-[11.5px] text-slate-500">
                {anyFilter ? "No events match these filters." : "No activity yet."}
            </div>
            {anyFilter && (
                <button
                    type="button"
                    onClick={onReset}
                    className="mt-2 h-6 px-2 rounded text-[11px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-100"
                >
                    Reset filters
                </button>
            )}
        </div>
    );
}

function SkeletonList() {
    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            {Array.from({ length: 6 }).map((_, i) => (
                <div
                    key={i}
                    className="px-3 py-2.5 border-b last:border-b-0 border-slate-100 flex items-start gap-2.5"
                    style={{ animationDelay: `${i * 60}ms` }}
                >
                    <div className="w-3.5 h-3.5 rounded bg-slate-100 mt-0.5 shrink-0 animate-pulse" />
                    <div className="flex-1 space-y-1.5">
                        <div
                            className="h-2.5 bg-slate-100 rounded animate-pulse"
                            style={{ width: `${40 + ((i * 13) % 40)}%` }}
                        />
                        <div
                            className="h-2 bg-slate-100/80 rounded animate-pulse"
                            style={{ width: `${30 + ((i * 11) % 50)}%` }}
                        />
                    </div>
                    <div className="h-2 w-10 bg-slate-100 rounded animate-pulse mt-1 shrink-0" />
                </div>
            ))}
        </div>
    );
}

function EventRow({
    event,
    highlight,
}: {
    event: ContactTimelineEvent;
    highlight: string;
}) {
    const { Icon, label } = visualFor(event.type);
    return (
        <div className="px-3 py-2 border-b last:border-b-0 border-slate-100">
            <div className="flex items-start gap-2.5">
                <Icon className="w-3.5 h-3.5 text-slate-400 mt-0.5 shrink-0" />
                <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-1.5 min-w-0">
                        <span className="text-[12px] font-medium text-slate-900 shrink-0">
                            {label}
                        </span>
                        {event.subject && (
                            <span className="text-[11.5px] text-slate-600 truncate">
                                · <Highlight text={event.subject} q={highlight} />
                            </span>
                        )}
                    </div>
                    <EventMeta event={event} highlight={highlight} />
                </div>
                <span
                    className="text-[10.5px] text-slate-400 tabular-nums shrink-0 mt-0.5"
                    title={fmtAbsolute(event.at)}
                >
                    {fmtRelative(event.at)}
                </span>
            </div>
            {event.content && (
                <div className="text-[11.5px] text-slate-700 mt-1.5 ml-6 whitespace-pre-wrap break-words border-l-2 border-slate-100 pl-2">
                    <Highlight text={event.content} q={highlight} />
                </div>
            )}
        </div>
    );
}

function EventMeta({
    event,
    highlight,
}: {
    event: ContactTimelineEvent;
    highlight: string;
}) {
    // Meetings get a dedicated meta line: when the call is set for, which
    // calendar it came from, and a one-click join link (when not canceled).
    if (event.type.startsWith("meeting_")) {
        const when = event.scheduled_for
            ? new Date(event.scheduled_for).toLocaleString(undefined, {
                  month: "short",
                  day: "numeric",
                  hour: "numeric",
                  minute: "2-digit",
              })
            : null;
        const providerLabel = event.source === "cal_com" ? "Cal.com" : event.source === "calendly" ? "Calendly" : event.source;
        return (
            <div className="text-[11px] text-slate-500 mt-0.5 flex gap-1.5 flex-wrap items-center">
                {when && <span>for {when}</span>}
                {when && providerLabel && <span className="text-slate-300">·</span>}
                {providerLabel && <span>via {providerLabel}</span>}
                {event.reason && (
                    <>
                        <span className="text-slate-300">·</span>
                        <span className="text-slate-700">{event.reason}</span>
                    </>
                )}
                {event.type !== "meeting_canceled" && event.join_url && (
                    <>
                        <span className="text-slate-300">·</span>
                        <a
                            href={event.join_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-sky-600 hover:text-sky-700 font-medium"
                        >
                            Join
                        </a>
                    </>
                )}
            </div>
        );
    }

    const parts: React.ReactNode[] = [];

    if (event.email_account_email) {
        parts.push(
            <span key="mailbox" className="font-mono">
                from{" "}
                <Highlight text={event.email_account_email} q={highlight} />
            </span>,
        );
    }
    if (event.campaign_name) {
        parts.push(
            <span key="campaign">
                in <Highlight text={event.campaign_name} q={highlight} />
            </span>,
        );
    }
    if (event.sequence_name) {
        parts.push(
            <span key="sequence">
                step <Highlight text={event.sequence_name} q={highlight} />
            </span>,
        );
    }
    if (event.intent) {
        parts.push(<span key="intent">intent: {event.intent}</span>);
    }
    if (event.provider && event.provider !== "manual") {
        parts.push(<span key="provider">via {event.provider}</span>);
    }
    if (event.source) {
        parts.push(<span key="source">type: {event.source}</span>);
    }
    if (event.reason) {
        parts.push(
            <span key="reason" className="text-slate-700">
                <Highlight text={event.reason} q={highlight} />
            </span>,
        );
    }

    if (parts.length === 0) return null;

    return (
        <div className="text-[11px] text-slate-500 mt-0.5 flex gap-1.5 flex-wrap">
            {parts.map((p, i) => (
                <React.Fragment key={i}>
                    {p}
                    {i < parts.length - 1 && (
                        <span className="text-slate-300">·</span>
                    )}
                </React.Fragment>
            ))}
        </div>
    );
}

function Highlight({ text, q }: { text: string; q: string }) {
    const ql = q.trim().toLowerCase();
    if (!ql) return <>{text}</>;
    const lower = text.toLowerCase();
    const idx = lower.indexOf(ql);
    if (idx < 0) return <>{text}</>;
    return (
        <>
            {text.slice(0, idx)}
            <mark className="bg-amber-100 text-amber-900 rounded-sm px-0.5">
                {text.slice(idx, idx + ql.length)}
            </mark>
            {text.slice(idx + ql.length)}
        </>
    );
}

function visualFor(type: ContactTimelineEventType): {
    Icon: typeof MailIcon;
    label: string;
} {
    switch (type) {
        case "email_sent":
            return { Icon: MailIcon, label: "Email sent" };
        case "email_opened":
            return { Icon: MailOpenIcon, label: "Opened" };
        case "email_clicked":
            return { Icon: MousePointerClickIcon, label: "Clicked link" };
        case "email_replied":
            return { Icon: ReplyIcon, label: "Replied" };
        case "reply_received":
            return { Icon: MessageSquareIcon, label: "Reply received" };
        case "email_bounced":
            return { Icon: MailWarningIcon, label: "Bounced" };
        case "deliverability":
            return { Icon: AlertOctagonIcon, label: "Deliverability event" };
        case "suppressed":
            return { Icon: BanIcon, label: "Suppressed" };
        case "note":
            return { Icon: StickyNoteIcon, label: "Note added" };
        case "meeting_booked":
            return { Icon: CalendarPlusIcon, label: "Meeting booked" };
        case "meeting_rescheduled":
            return { Icon: CalendarClockIcon, label: "Meeting rescheduled" };
        case "meeting_canceled":
            return { Icon: CalendarXIcon, label: "Meeting canceled" };
        default:
            return { Icon: MailIcon, label: type };
    }
}

function fmtChip(d: string): string {
    if (!d) return "";
    const dt = new Date(d + "T00:00:00");
    if (Number.isNaN(dt.getTime())) return d;
    return dt.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function toInput(d: Date): string {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
}
