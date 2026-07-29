// OutboxIndicator — the header "undo send" pill. Instant sends wait a few
// seconds server-side; while any are pending this shows a live countdown
// for the soonest one plus a Cancel affordance, and clicking the pill opens
// a portaled list of every pending send with per-row cancel. Cancelling a
// compose reopens the composer from its seed; cancelling a reply hands the
// payload back to the thread's reply composer and navigates there.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, SendIcon } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import cancelScheduled from "@/lib/api/client/app/unibox/cancelScheduled";
import { useOutboxStore, type OutboxEntry } from "@/hooks/useOutboxStore";
import { useComposeStore } from "@/hooks/useComposeStore";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

function secondsLeft(entry: OutboxEntry, now: number): number {
    return Math.max(0, Math.ceil((entry.scheduledAt - now) / 1000));
}

export default function OutboxIndicator() {
    const entries = useOutboxStore((s) => s.entries);
    const remove = useOutboxStore((s) => s.remove);
    const navigate = useNavigate();
    const queryClient = useQueryClient();

    const [now, setNow] = React.useState(() => Date.now());
    const [open, setOpen] = React.useState(false);
    const [anchor, setAnchor] = React.useState<{ top: number; right: number } | null>(null);
    const [cancelling, setCancelling] = React.useState<Set<string>>(() => new Set());
    const cancellingRef = React.useRef(cancelling);
    cancellingRef.current = cancelling;
    const pillRef = React.useRef<HTMLDivElement>(null);
    const panelRef = React.useRef<HTMLDivElement>(null);

    // One shared ticker while anything is pending.
    React.useEffect(() => {
        if (entries.length === 0) return;
        setNow(Date.now());
        const id = window.setInterval(() => setNow(Date.now()), 500);
        return () => window.clearInterval(id);
    }, [entries.length]);

    // An entry hitting 0 means it sent; drop it (unless a cancel for it is
    // still in flight, so the row doesn't vanish mid-request).
    React.useEffect(() => {
        for (const e of entries) {
            if (e.scheduledAt <= now && !cancellingRef.current.has(e.taskId)) {
                remove(e.taskId);
            }
        }
    }, [now, entries, remove]);

    React.useEffect(() => {
        if (entries.length === 0) setOpen(false);
    }, [entries.length]);

    // Outside click / Escape closes the dropdown (same shape as FilterMenu).
    React.useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent | TouchEvent) => {
            const t = e.target as Node;
            if (pillRef.current?.contains(t)) return;
            if (panelRef.current?.contains(t)) return;
            setOpen(false);
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.stopPropagation();
                setOpen(false);
            }
        };
        document.addEventListener("mousedown", onDown, true);
        document.addEventListener("touchstart", onDown, true);
        document.addEventListener("keydown", onKey, true);
        return () => {
            document.removeEventListener("mousedown", onDown, true);
            document.removeEventListener("touchstart", onDown, true);
            document.removeEventListener("keydown", onKey, true);
        };
    }, [open]);

    const sorted = React.useMemo(
        () => [...entries].sort((a, b) => a.scheduledAt - b.scheduledAt),
        [entries],
    );
    const soonest = sorted[0];

    const toggle = () => {
        const el = pillRef.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        setAnchor({
            top: r.bottom + 6,
            right: Math.max(8, document.documentElement.clientWidth - r.right),
        });
        setOpen((o) => !o);
    };

    const cancel = async (entry: OutboxEntry) => {
        if (cancellingRef.current.has(entry.taskId)) return;
        setCancelling((prev) => new Set(prev).add(entry.taskId));
        try {
            await cancelScheduled(entry.taskId);
            remove(entry.taskId);
            void queryClient.invalidateQueries({ queryKey: ["unibox"] });
            if (entry.kind === "compose" && entry.seed) {
                useComposeStore.getState().openDraft(entry.seed);
                toast.success("Send cancelled, back to your draft");
            } else if (entry.kind === "reply" && entry.reply) {
                useOutboxStore.getState().setReplyRestore(entry.reply);
                navigate(`/app/unibox/all/${encodeURIComponent(entry.reply.threadId)}`);
                toast.success("Send cancelled, back to your reply");
            } else {
                toast.success("Send cancelled");
            }
        } catch (e) {
            const err = e as AppError;
            if (err?.status === 404) {
                // The task already fired; nothing left to cancel.
                remove(entry.taskId);
                void queryClient.invalidateQueries({ queryKey: ["unibox"] });
                toast.error("Already sent");
            } else {
                toast.error(buildError(err));
            }
        } finally {
            setCancelling((prev) => {
                const next = new Set(prev);
                next.delete(entry.taskId);
                return next;
            });
        }
    };

    return (
        <>
            <AnimatePresence>
                {soonest && (
                    <motion.div
                        initial={{ opacity: 0, scale: 0.94 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.94 }}
                        transition={{ duration: 0.14, ease: [0.16, 1, 0.3, 1] }}
                        ref={pillRef}
                        className="h-7 pl-2 pr-1 rounded-md border border-amber-200 bg-amber-50 flex items-center gap-1 shrink-0"
                    >
                        <button
                            type="button"
                            onClick={toggle}
                            title="Pending sends"
                            className="h-full flex items-center gap-1.5 text-[12.5px] text-amber-800 hover:text-amber-950 transition-colors"
                        >
                            <SendIcon className="w-3.5 h-3.5 shrink-0" />
                            <span className="font-medium tabular-nums whitespace-nowrap">
                                Sending in {secondsLeft(soonest, now)}s
                            </span>
                            {sorted.length > 1 && (
                                <span className="h-4 px-1 rounded bg-amber-100 text-[10px] font-semibold text-amber-800 inline-flex items-center">
                                    +{sorted.length - 1}
                                </span>
                            )}
                        </button>
                        <button
                            type="button"
                            onClick={() => void cancel(soonest)}
                            disabled={cancelling.has(soonest.taskId)}
                            className="h-5 px-1.5 rounded bg-white/70 border border-amber-200 text-[10.5px] font-medium text-amber-900 hover:bg-white transition-colors disabled:opacity-50"
                        >
                            {cancelling.has(soonest.taskId) ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : (
                                "Cancel"
                            )}
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
            {createPortal(
                <AnimatePresence>
                    {open && anchor && sorted.length > 0 && (
                        <motion.div
                            ref={panelRef}
                            data-floating=""
                            initial={{ opacity: 0, y: -4, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, y: -4, scale: 0.98 }}
                            transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                            style={{
                                position: "fixed",
                                top: anchor.top,
                                right: anchor.right,
                                zIndex: 130,
                            }}
                            className="w-[300px] max-w-[calc(100vw-16px)] max-h-[300px] overflow-y-auto rounded-md border border-slate-200 bg-white shadow-xl py-1"
                        >
                            <div className="px-2.5 pt-1 pb-1.5 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Pending sends
                            </div>
                            {sorted.map((entry) => (
                                <div
                                    key={entry.taskId}
                                    className="px-2.5 py-1.5 flex items-center gap-2 hover:bg-slate-50 transition-colors"
                                >
                                    <div className="min-w-0 flex-1">
                                        <div className="text-[11.5px] text-slate-800 truncate">
                                            {entry.to[0] ?? "(no recipient)"}
                                            {entry.to.length > 1 && (
                                                <span className="text-slate-400"> +{entry.to.length - 1}</span>
                                            )}
                                        </div>
                                        <div className="text-[10.5px] text-slate-400 truncate">
                                            {entry.subject || "(no subject)"}
                                        </div>
                                    </div>
                                    <span className="shrink-0 tabular-nums text-[11px] font-medium text-amber-700">
                                        {secondsLeft(entry, now)}s
                                    </span>
                                    <button
                                        type="button"
                                        onClick={() => void cancel(entry)}
                                        disabled={cancelling.has(entry.taskId)}
                                        className="shrink-0 h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-50 inline-flex items-center"
                                    >
                                        {cancelling.has(entry.taskId) ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin" />
                                        ) : (
                                            "Cancel"
                                        )}
                                    </button>
                                </div>
                            ))}
                        </motion.div>
                    )}
                </AnimatePresence>,
                document.body,
            )}
        </>
    );
}
