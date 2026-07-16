// The in-app notification bell in the dashboard chrome. Reads the persisted feed
// (source of truth for unread state) + refreshes live on the realtime
// NOTIFICATION event (wired in useRealtimeEvents).

import React from "react";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { BellIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import {
    useNotifications,
    useMarkAllNotificationsRead,
    useMarkNotificationRead,
} from "@/lib/api/hooks/app/notifications/useNotifications";
import type { AppNotification } from "@/lib/api/models/app/notifications/Notification";

export function NotificationBell() {
    const { data } = useNotifications();
    const markAll = useMarkAllNotificationsRead();
    const markOne = useMarkNotificationRead();
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    const close = React.useCallback(() => setOpen(false), []);
    useClickOutside(ref, close);

    const unread = data?.unread ?? 0;
    const items = data?.notifications ?? [];

    const itemBody = (n: AppNotification) => (
        <div className="flex items-start gap-2">
            {!n.read_at && <span className="mt-1.5 size-1.5 rounded-full bg-sky-500 shrink-0" />}
            <div className={`min-w-0 ${n.read_at ? "opacity-60" : ""}`}>
                <div className="text-[12px] font-medium text-slate-800 truncate">{n.title}</div>
                {n.body && <div className="text-[11px] text-slate-500 truncate">{n.body}</div>}
                <div className="text-[10px] text-slate-400 mt-0.5">{new Date(n.created_at).toLocaleString()}</div>
            </div>
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
                        className="absolute right-0 top-full mt-1.5 w-80 max-w-[90vw] rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] z-50 overflow-hidden"
                    >
                    <div className="h-9 px-3 flex items-center border-b border-slate-200">
                        <span className="text-[12px] font-medium text-slate-900">Notifications</span>
                        {unread > 0 && (
                            <button
                                type="button"
                                onClick={() => markAll.mutate()}
                                className="ml-auto text-[11px] text-sky-600 hover:text-sky-700"
                            >
                                Mark all read
                            </button>
                        )}
                    </div>
                    <div className="max-h-96 overflow-y-auto">
                        {items.length === 0 ? (
                            <div className="px-3 py-6 text-center text-[12px] text-slate-400">You&apos;re all caught up.</div>
                        ) : (
                            items.map((n) => {
                                const click = () => {
                                    if (!n.read_at) markOne.mutate(n.id);
                                    setOpen(false);
                                };
                                return n.link ? (
                                    <Link
                                        key={n.id}
                                        to={n.link}
                                        onClick={click}
                                        className="block px-3 py-2 hover:bg-slate-50 border-b border-slate-100 last:border-0"
                                    >
                                        {itemBody(n)}
                                    </Link>
                                ) : (
                                    <button
                                        key={n.id}
                                        type="button"
                                        onClick={click}
                                        className="block w-full text-left px-3 py-2 hover:bg-slate-50 border-b border-slate-100 last:border-0"
                                    >
                                        {itemBody(n)}
                                    </button>
                                );
                            })
                        )}
                    </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
