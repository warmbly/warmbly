// Left-rail navigator for the unibox.
//
// Reads /unibox/overview so every count is server-truth. Three
// sections:
//
//   1. Inbox — All / Unread / Today / Week / Awaiting reply / Snoozed
//   2. Mailboxes — every connected account with its unread count.
//      Collapses to 6 once the user has more than 8 (with a
//      "Show all (N)" toggle) and adds an in-section search so the
//      rail never becomes a wall of scrolling.
//   3. Tags — same collapse/search treatment.

import React from "react";
import {
    CalendarRangeIcon,
    ChevronDownIcon,
    ChevronRightIcon,
    ClockIcon,
    InboxIcon,
    MailboxIcon,
    MoonIcon,
    ReplyIcon,
    SearchIcon,
    SendIcon,
    SparkleIcon,
} from "lucide-react";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import { cn } from "@/lib/utils";

export type UniboxScope =
    | { kind: "all" }
    | { kind: "unread" }
    | { kind: "today" }
    | { kind: "week" }
    | { kind: "awaiting" }
    | { kind: "snoozed" }
    | { kind: "scheduled" }
    | { kind: "mailbox"; mailboxId: string }
    | { kind: "tag"; tagId: string };

export function scopeKey(s: UniboxScope): string {
    switch (s.kind) {
        case "mailbox":
            return `mailbox:${s.mailboxId}`;
        case "tag":
            return `tag:${s.tagId}`;
        default:
            return s.kind;
    }
}

const COLLAPSE_THRESHOLD = 8;
const COLLAPSED_VISIBLE = 6;

interface ScopeRailProps {
    scope: UniboxScope;
    onChange: (s: UniboxScope) => void;
}

export function ScopeRail({ scope, onChange }: ScopeRailProps) {
    const overview = useUniboxOverview();
    const data = overview.data;

    const active = scopeKey(scope);

    return (
        <nav className="h-full bg-slate-50/60 border-r border-slate-200 overflow-y-auto py-2">
            <Section label="Inbox">
                <Item
                    icon={<InboxIcon className="w-3.5 h-3.5" />}
                    label="All"
                    count={data?.total}
                    active={active === "all"}
                    onClick={() => onChange({ kind: "all" })}
                />
                <Item
                    icon={<SparkleIcon className="w-3.5 h-3.5" />}
                    label="Unread"
                    count={data?.unread}
                    countTone={data?.unread ? "accent" : "muted"}
                    active={active === "unread"}
                    onClick={() => onChange({ kind: "unread" })}
                />
                <Item
                    icon={<ClockIcon className="w-3.5 h-3.5" />}
                    label="Today"
                    count={data?.today}
                    active={active === "today"}
                    onClick={() => onChange({ kind: "today" })}
                />
                <Item
                    icon={<CalendarRangeIcon className="w-3.5 h-3.5" />}
                    label="This week"
                    count={data?.week}
                    active={active === "week"}
                    onClick={() => onChange({ kind: "week" })}
                />
                <Item
                    icon={<ReplyIcon className="w-3.5 h-3.5" />}
                    label="Awaiting reply"
                    count={data?.awaiting_reply}
                    countTone={data?.awaiting_reply ? "accent" : "muted"}
                    active={active === "awaiting"}
                    onClick={() => onChange({ kind: "awaiting" })}
                />
                <Item
                    icon={<MoonIcon className="w-3.5 h-3.5" />}
                    label="Snoozed"
                    count={data?.snoozed}
                    active={active === "snoozed"}
                    onClick={() => onChange({ kind: "snoozed" })}
                />
                <Item
                    icon={<SendIcon className="w-3.5 h-3.5" />}
                    label="Scheduled"
                    count={data?.scheduled_pending}
                    countTone={data?.scheduled_pending ? "accent" : "muted"}
                    active={active === "scheduled"}
                    onClick={() => onChange({ kind: "scheduled" })}
                />
                {/* Cap meter — only when the user is materially through the
                    allowance, so the rail stays calm for the 99% case. */}
                {data &&
                    data.scheduled_pending_max > 0 &&
                    data.scheduled_pending / data.scheduled_pending_max >= 0.7 && (
                        <div className="px-2 pt-1 pb-1.5">
                            <ScheduledMeter
                                used={data.scheduled_pending}
                                cap={data.scheduled_pending_max}
                            />
                        </div>
                    )}
            </Section>

            <CollapsibleSection
                label="Mailboxes"
                items={data?.mailboxes ?? []}
                emptyText={overview.isPending ? "Loading…" : "No mailboxes connected."}
                searchPlaceholder="Filter mailboxes…"
                getSearchKey={(m) => `${m.email} ${m.name}`}
                renderItem={(m) => (
                    <Item
                        key={m.id}
                        icon={<MailboxIcon className="w-3.5 h-3.5" />}
                        label={m.email}
                        mono
                        count={m.unread || undefined}
                        countTone={m.unread > 0 ? "accent" : "muted"}
                        active={active === `mailbox:${m.id}`}
                        onClick={() => onChange({ kind: "mailbox", mailboxId: m.id })}
                    />
                )}
            />

            {data && data.tags.length > 0 && (
                <CollapsibleSection
                    label="Tags"
                    items={data.tags}
                    emptyText="No tags yet."
                    searchPlaceholder="Filter tags…"
                    getSearchKey={(t) => t.title}
                    renderItem={(t) => (
                        <Item
                            key={t.id}
                            icon={
                                <span
                                    aria-hidden
                                    className="block size-3 rounded-full ring-1 ring-black/10 shadow-sm"
                                    style={{ backgroundColor: t.color || "#94a3b8" }}
                                />
                            }
                            label={t.title}
                            count={t.unread || t.total || undefined}
                            countTone={t.unread > 0 ? "accent" : "muted"}
                            active={active === `tag:${t.id}`}
                            onClick={() => onChange({ kind: "tag", tagId: t.id })}
                        />
                    )}
                />
            )}
        </nav>
    );
}

function CollapsibleSection<T extends { id: string }>({
    label,
    items,
    emptyText,
    searchPlaceholder,
    getSearchKey,
    renderItem,
}: {
    label: string;
    items: T[];
    emptyText: string;
    searchPlaceholder: string;
    getSearchKey: (item: T) => string;
    renderItem: (item: T) => React.ReactNode;
}) {
    const [search, setSearch] = React.useState("");
    const [expanded, setExpanded] = React.useState(false);
    const [sectionOpen, setSectionOpen] = React.useState(true);

    const filtered = React.useMemo(() => {
        const q = search.trim().toLowerCase();
        if (!q) return items;
        return items.filter((it) => getSearchKey(it).toLowerCase().includes(q));
    }, [items, search, getSearchKey]);

    const showSearch = items.length > COLLAPSE_THRESHOLD;
    const showCollapse = filtered.length > COLLAPSE_THRESHOLD;
    const visible = showCollapse && !expanded ? filtered.slice(0, COLLAPSED_VISIBLE) : filtered;
    const hidden = filtered.length - visible.length;

    return (
        <div className="mb-1">
            <button
                type="button"
                onClick={() => setSectionOpen((v) => !v)}
                className="w-full px-3 pt-3 pb-1 flex items-center gap-1.5 text-left"
            >
                {sectionOpen ? (
                    <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                ) : (
                    <ChevronRightIcon className="w-3 h-3 text-slate-400" />
                )}
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold">
                    {label}
                </span>
                <span className="ml-auto font-mono text-[10px] text-slate-400 tabular-nums">
                    {items.length}
                </span>
            </button>

            {sectionOpen && (
                <div className="px-1.5">
                    {showSearch && (
                        <div className="flex items-center gap-1.5 px-2 py-1 mb-1 rounded-md border border-slate-200 bg-white">
                            <SearchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                            <input
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                placeholder={searchPlaceholder}
                                className="flex-1 min-w-0 h-5 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                            {search && (
                                <button
                                    type="button"
                                    onClick={() => setSearch("")}
                                    className="text-[10px] text-slate-400 hover:text-slate-600 shrink-0"
                                    aria-label="Clear filter"
                                >
                                    clear
                                </button>
                            )}
                        </div>
                    )}

                    {items.length === 0 ? (
                        <div className="px-2 py-2 text-[11px] text-slate-400">{emptyText}</div>
                    ) : filtered.length === 0 ? (
                        <div className="px-2 py-2 text-[11px] text-slate-400">No matches.</div>
                    ) : (
                        <div className="space-y-px">{visible.map(renderItem)}</div>
                    )}

                    {hidden > 0 && (
                        <button
                            type="button"
                            onClick={() => setExpanded(true)}
                            className="w-full h-7 px-2 mt-1 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-white/70 transition-colors text-left"
                        >
                            Show all ({hidden} more)
                        </button>
                    )}
                    {expanded && filtered.length > COLLAPSED_VISIBLE && (
                        <button
                            type="button"
                            onClick={() => setExpanded(false)}
                            className="w-full h-7 px-2 mt-1 rounded-md text-[11.5px] text-slate-400 hover:text-slate-700 hover:bg-white/70 transition-colors text-left"
                        >
                            Show less
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}

// ScheduledMeter — slim usage bar that surfaces the pending-cap. Stays
// hidden until the user is at ≥70% so the rail isn't cluttered for the
// common case where the cap is irrelevant. Shifts to amber/red as the
// cap gets close so the user gets unmissable warning before sends fail.
function ScheduledMeter({ used, cap }: { used: number; cap: number }) {
    const ratio = Math.min(1, used / cap);
    const pct = Math.round(ratio * 100);
    const tone =
        ratio >= 0.95 ? "rose" : ratio >= 0.85 ? "amber" : "sky";

    const barClasses =
        tone === "rose"
            ? "bg-rose-500"
            : tone === "amber"
                ? "bg-amber-500"
                : "bg-sky-500";
    const textClasses =
        tone === "rose"
            ? "text-rose-700"
            : tone === "amber"
                ? "text-amber-700"
                : "text-slate-500";

    return (
        <div className="px-1">
            <div className={cn("flex items-center justify-between text-[10px] mb-1", textClasses)}>
                <span className="uppercase tracking-[0.14em] font-medium">Queue</span>
                <span className="font-mono tabular-nums">
                    {used}/{cap}
                </span>
            </div>
            <div className="h-1 rounded-full bg-slate-200 overflow-hidden">
                <div
                    className={cn("h-full transition-all", barClasses)}
                    style={{ width: `${pct}%` }}
                />
            </div>
            {ratio >= 0.95 && (
                <p className="mt-1 text-[10px] text-rose-600 leading-snug">
                    Near the limit — cancel a few sends to free up space.
                </p>
            )}
        </div>
    );
}

function Section({
    label,
    children,
}: {
    label: string;
    children: React.ReactNode;
}) {
    return (
        <div className="mb-1">
            <div className="px-3 pt-3 pb-1 flex items-center gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold">
                    {label}
                </span>
            </div>
            <div className="px-1.5 space-y-px">{children}</div>
        </div>
    );
}

function Item({
    icon,
    label,
    count,
    countTone = "muted",
    active,
    mono,
    onClick,
}: {
    icon: React.ReactNode;
    label: string;
    count?: number;
    countTone?: "muted" | "accent";
    active?: boolean;
    mono?: boolean;
    onClick: () => void;
}) {
    // Active = sky-100 (clearly distinct from the slate-50 rail bg).
    // Hover = slate-200/70 so it reads as "pressable" on the muted rail
    // — the previous white-on-slate combo was invisible.
    return (
        <button
            type="button"
            onClick={onClick}
            className={cn(
                "w-full h-7 pl-2 pr-2 rounded-md flex items-center gap-2 transition-colors text-left",
                active
                    ? "bg-sky-100 text-sky-900 font-medium"
                    : "text-slate-600 hover:bg-slate-200/70 hover:text-slate-900",
            )}
            title={label}
        >
            <span className={cn("shrink-0", active ? "text-sky-700" : "text-slate-500")}>
                {icon}
            </span>
            <span
                className={cn(
                    "truncate min-w-0 flex-1 text-[12px]",
                    mono && "font-mono text-[11.5px]",
                )}
            >
                {label}
            </span>
            {count !== undefined && count !== null && (
                <span
                    className={cn(
                        "shrink-0 font-mono tabular-nums text-[10.5px] px-1.5 h-4 rounded inline-flex items-center",
                        active
                            ? "bg-white/80 text-sky-700"
                            : countTone === "accent"
                                ? "bg-sky-100 text-sky-700"
                                : "text-slate-400",
                    )}
                >
                    {count}
                </span>
            )}
        </button>
    );
}
