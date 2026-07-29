// The in-app notification bell in the dashboard chrome. Reads the persisted feed
// (source of truth for unread state) + refreshes live on the realtime
// NOTIFICATION event (wired in useRealtimeEvents).

import React from "react";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    BellIcon,
    ClockIcon,
    CreditCardIcon,
    KeyRoundIcon,
    ReplyIcon,
    ServerCrashIcon,
    Settings2Icon,
    ShieldAlertIcon,
    TriangleAlertIcon,
    UsersIcon,
    type LucideIcon,
} from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import {
    useNotifications,
    useMarkAllNotificationsRead,
    useMarkNotificationRead,
} from "@/lib/api/hooks/app/notifications/useNotifications";
import type { AppNotification } from "@/lib/api/models/app/notifications/Notification";

// Per-category icon + tone so the list scans by kind before reading titles.
const CATEGORY_META: Record<string, { icon: LucideIcon; tone: string }> = {
    inbound_reply: { icon: ReplyIcon, tone: "bg-sky-50 text-sky-600" },
    inbound_out_of_office: { icon: ClockIcon, tone: "bg-slate-100 text-slate-500" },
    health_bounce: { icon: TriangleAlertIcon, tone: "bg-amber-50 text-amber-600" },
    health_complaint: { icon: ShieldAlertIcon, tone: "bg-rose-50 text-rose-600" },
    health_worker_downtime: { icon: ServerCrashIcon, tone: "bg-rose-50 text-rose-600" },
    security_new_signin: { icon: KeyRoundIcon, tone: "bg-violet-50 text-violet-600" },
    billing_alert: { icon: CreditCardIcon, tone: "bg-amber-50 text-amber-600" },
    team_activity: { icon: UsersIcon, tone: "bg-emerald-50 text-emerald-600" },
};

const FALLBACK_META = { icon: BellIcon, tone: "bg-slate-100 text-slate-500" };

type Bucket = "today" | "yesterday" | "earlier";

const BUCKET_LABELS: Record<Bucket, string> = {
    today: "Today",
    yesterday: "Yesterday",
    earlier: "Earlier",
};

function startOfToday(): number {
    const now = new Date();
    return new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
}

function bucketFor(d: Date): Bucket {
    const today = startOfToday();
    const yesterday = today - 24 * 60 * 60 * 1000;
    const t = d.getTime();
    if (t >= today) return "today";
    if (t >= yesterday) return "yesterday";
    return "earlier";
}

// Compact relative timestamp: "now", "2m", "3h", "Yesterday", then a short date.
function relTime(iso: string): string {
    const d = new Date(iso);
    const s = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
    if (s < 60) return "now";
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h`;
    if (bucketFor(d) === "yesterday") return "Yesterday";
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export function NotificationBell() {
    const { data, isLoading } = useNotifications();
    const markAll = useMarkAllNotificationsRead();
    const markOne = useMarkNotificationRead();
    const [open, setOpen] = React.useState(false);
    const [filter, setFilter] = React.useState<"all" | "unread">("all");
    const ref = React.useRef<HTMLDivElement>(null);
    const close = React.useCallback(() => setOpen(false), []);
    useClickOutside(ref, close);

    const unread = data?.unread ?? 0;
    const items = data?.notifications ?? [];
    const visible = filter === "unread" ? items.filter((n) => !n.read_at) : items;

    // Group by day bucket; the feed is newest → oldest, so one pass keeps
    // both global order and group adjacency (same shape as the unibox list).
    const grouped = React.useMemo(() => {
        const groups: { bucket: Bucket; rows: AppNotification[] }[] = [];
        for (const n of visible) {
            const b = bucketFor(new Date(n.created_at));
            const tail = groups[groups.length - 1];
            if (tail && tail.bucket === b) tail.rows.push(n);
            else groups.push({ bucket: b, rows: [n] });
        }
        return groups;
    }, [visible]);

    const itemBody = (n: AppNotification) => {
        const meta = CATEGORY_META[n.category] ?? FALLBACK_META;
        const Icon = meta.icon;
        return (
            <div className="flex items-start gap-2.5">
                <span className={`mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md ${meta.tone}`}>
                    <Icon className="size-3.5" />
                </span>
                <div className="min-w-0 flex-1">
                    <div
                        className={`text-[12px] truncate ${
                            n.read_at ? "font-normal text-slate-600" : "font-semibold text-slate-900"
                        }`}
                    >
                        {n.title}
                    </div>
                    {n.body && (
                        <div className={`text-[11px] truncate ${n.read_at ? "text-slate-400" : "text-slate-500"}`}>
                            {n.body}
                        </div>
                    )}
                </div>
                <div className="flex shrink-0 items-center gap-1.5 pt-0.5">
                    <span className="text-[10px] text-slate-400">{relTime(n.created_at)}</span>
                    {!n.read_at && <span className="size-1.5 rounded-full bg-sky-500" />}
                </div>
            </div>
        );
    };

    const rowClass = (n: AppNotification) =>
        `block w-full text-left px-3 py-2 border-l-2 transition-colors hover:bg-slate-50 ${
            n.read_at ? "border-l-transparent" : "border-l-sky-500 bg-sky-50/40"
        }`;

    const emptyState = (
        <div className="px-3 py-8 flex flex-col items-center gap-2 text-center">
            <span className="flex size-8 items-center justify-center rounded-full bg-slate-100 text-slate-400">
                <BellIcon className="size-4" />
            </span>
            <span className="text-[12px] text-slate-400">You&apos;re all caught up.</span>
        </div>
    );

    const skeleton = (
        <div className="px-3 py-2.5 space-y-3 animate-pulse" aria-hidden="true">
            {[0, 1, 2, 3].map((i) => (
                <div key={i} className="flex items-start gap-2.5">
                    <div className="size-6 rounded-md bg-slate-100" />
                    <div className="flex-1 space-y-1.5 pt-0.5">
                        <div className="h-2.5 w-2/3 rounded bg-slate-100" />
                        <div className="h-2 w-1/2 rounded bg-slate-100" />
                    </div>
                </div>
            ))}
        </div>
    );

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                aria-label="Notifications"
                className="relative w-7 h-7 rounded-md flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 transition-colors"
            >
                <BellIcon className="w-4 h-4" />
                {unread > 0 && (
                    <span className="absolute -top-0.5 -right-0.5 min-w-[14px] h-[14px] px-1 rounded-full bg-rose-500 text-white text-[9px] font-semibold flex items-center justify-center">
                        {unread > 9 ? "9+" : unread}
                    </span>
                )}
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute right-0 top-full mt-1.5 w-[340px] max-w-[90vw] rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] z-50 overflow-hidden"
                    >
                        <div className="h-9 px-3 flex items-center gap-2 border-b border-slate-200">
                            <span className="text-[12px] font-medium text-slate-900">Notifications</span>
                            <div className="ml-auto flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
                                {(["all", "unread"] as const).map((f) => (
                                    <button
                                        key={f}
                                        type="button"
                                        onClick={() => setFilter(f)}
                                        className={`h-5 px-1.5 rounded text-[10.5px] font-medium transition-colors ${
                                            filter === f
                                                ? "bg-white text-slate-900 shadow-sm"
                                                : "text-slate-500 hover:text-slate-900"
                                        }`}
                                    >
                                        {f === "all" ? "All" : "Unread"}
                                    </button>
                                ))}
                            </div>
                            {unread > 0 && (
                                <button
                                    type="button"
                                    onClick={() => markAll.mutate()}
                                    className="text-[11px] text-sky-600 hover:text-sky-700 shrink-0"
                                >
                                    Mark all read
                                </button>
                            )}
                        </div>
                        <div className="max-h-96 overflow-y-auto">
                            {isLoading ? (
                                skeleton
                            ) : visible.length === 0 ? (
                                emptyState
                            ) : (
                                grouped.map((g) => (
                                    <div key={g.bucket}>
                                        <div className="sticky top-0 z-10 px-3 pt-2 pb-1 bg-white text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-400">
                                            {BUCKET_LABELS[g.bucket]}
                                        </div>
                                        {g.rows.map((n) => {
                                            const click = () => {
                                                if (!n.read_at) markOne.mutate(n.id);
                                                setOpen(false);
                                            };
                                            return n.link ? (
                                                <Link key={n.id} to={n.link} onClick={click} className={rowClass(n)}>
                                                    {itemBody(n)}
                                                </Link>
                                            ) : (
                                                <button key={n.id} type="button" onClick={click} className={rowClass(n)}>
                                                    {itemBody(n)}
                                                </button>
                                            );
                                        })}
                                    </div>
                                ))
                            )}
                        </div>
                        <div className="border-t border-slate-200 bg-slate-50/60">
                            <Link
                                to="/app/settings/notifications"
                                onClick={close}
                                className="flex h-8 items-center justify-center gap-1.5 text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors"
                            >
                                <Settings2Icon className="size-3.5" />
                                Notification settings
                            </Link>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
