// MailboxPicker — the compose "From" selector. Defaults to Auto: the backend
// scores every active mailbox for the current recipient (conversation
// affinity, remaining daily budget, domain auth) and this control shows the
// resolved pick with its reason. Opening the menu reveals every candidate
// with its budget bar, history badge, and auth health so a manual override
// is an informed choice, not a guess.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    MessagesSquareIcon,
    ShieldAlertIcon,
    SparklesIcon,
} from "lucide-react";
import type { ComposeCandidate, ComposeCandidatesResponse } from "@/lib/api/models/app/unibox/Compose";
import useClickOutside from "@/hooks/useClickOutside";
import { cn } from "@/lib/utils";

interface MailboxPickerProps {
    // "auto" or an account id.
    value: string;
    onChange: (next: string) => void;
    candidates: ComposeCandidatesResponse | undefined;
    // True while candidates refetch for a new recipient.
    loading?: boolean;
}

const PANEL_WIDTH = 320;

export default function MailboxPicker({ value, onChange, candidates, loading }: MailboxPickerProps) {
    const [open, setOpen] = React.useState(false);
    // Viewport anchor for the portaled panel (the compose window clips
    // overflow, so the menu can't render inside it).
    const [anchor, setAnchor] = React.useState<{ top: number; left: number; up: boolean } | null>(null);
    const boxRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(boxRef, () => setOpen(false));

    const measure = React.useCallback(() => {
        const el = boxRef.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        const vw = window.innerWidth;
        const vh = window.innerHeight;
        const left = Math.min(Math.max(r.left, 8), vw - PANEL_WIDTH - 8);
        // Flip above the trigger when the space below is tight.
        const up = vh - r.bottom < 320 && r.top > vh - r.bottom;
        setAnchor({ top: up ? r.top - 4 : r.bottom + 4, left, up });
    }, []);

    React.useEffect(() => {
        if (!open) return;
        measure();
        window.addEventListener("scroll", measure, true);
        window.addEventListener("resize", measure);
        return () => {
            window.removeEventListener("scroll", measure, true);
            window.removeEventListener("resize", measure);
        };
    }, [open, measure]);

    const accounts = candidates?.accounts ?? [];
    const recommended = accounts.find((a) => a.recommended) ?? accounts[0];
    const selected = value === "auto" ? recommended : accounts.find((a) => a.id === value);

    return (
        <div ref={boxRef} className="relative min-w-0 flex-1">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="group max-w-full inline-flex items-center gap-1.5 h-6 pl-1 pr-1 -ml-1 rounded-md hover:bg-slate-50 transition-colors min-w-0"
            >
                {value === "auto" ? (
                    <>
                        {selected ? (
                            <span className="text-[12.5px] text-slate-800 truncate">
                                {selected.email}
                            </span>
                        ) : (
                            <span className="text-[12px] text-slate-400">
                                {loading ? "picking a mailbox…" : "no active mailbox"}
                            </span>
                        )}
                        <span className="h-4 px-1 rounded bg-slate-100 text-slate-500 text-[9.5px] font-medium uppercase tracking-wide shrink-0">
                            auto
                        </span>
                    </>
                ) : selected ? (
                    <>
                        <span className="text-[12.5px] text-slate-900 font-medium truncate">
                            {selected.name || selected.email}
                        </span>
                        <span className="font-mono text-[10.5px] text-slate-500 truncate">
                            {selected.email}
                        </span>
                    </>
                ) : (
                    <span className="text-[12px] text-amber-700">mailbox unavailable</span>
                )}
                <ChevronDownIcon className="w-3 h-3 text-slate-300 shrink-0 group-hover:text-slate-500 transition-colors" />
            </button>

            {createPortal(
            <AnimatePresence>
                {open && anchor && (
                    <motion.div
                        data-floating=""
                        initial={{ opacity: 0, y: anchor.up ? 4 : -4, scale: 0.98 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: anchor.up ? 4 : -4, scale: 0.98 }}
                        transition={{ duration: 0.14, ease: [0.16, 1, 0.3, 1] }}
                        style={{
                            position: "fixed",
                            left: anchor.left,
                            width: PANEL_WIDTH,
                            zIndex: 120,
                            ...(anchor.up
                                ? { bottom: window.innerHeight - anchor.top }
                                : { top: anchor.top }),
                        }}
                        className="max-w-[calc(100vw-16px)] rounded-lg border border-slate-200 bg-white shadow-xl overflow-hidden"
                    >
                        <button
                            type="button"
                            onClick={() => {
                                onChange("auto");
                                setOpen(false);
                            }}
                            className={cn(
                                "w-full px-3 py-2 flex items-start gap-2 text-left transition-colors hover:bg-slate-50",
                                value === "auto" && "bg-sky-50/60",
                            )}
                        >
                            <span className="size-6 rounded-md bg-sky-100 text-sky-600 inline-flex items-center justify-center shrink-0 mt-0.5">
                                <SparklesIcon className="w-3.5 h-3.5" />
                            </span>
                            <span className="min-w-0 flex-1">
                                <span className="block text-[12.5px] font-medium text-slate-900">
                                    Auto
                                    {value === "auto" && (
                                        <CheckIcon className="inline w-3 h-3 ml-1.5 text-sky-600" />
                                    )}
                                </span>
                                <span className="block text-[11px] text-slate-500 leading-snug">
                                    {recommended
                                        ? `Picks ${recommended.email}${candidates?.recommended_reason ? `: ${candidates.recommended_reason}` : ""}`
                                        : "Picks the best mailbox for each recipient"}
                                </span>
                            </span>
                        </button>

                        <div className="border-t border-slate-100 max-h-56 overflow-y-auto">
                            {accounts.length === 0 && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400">
                                    No active mailboxes. Connect one under Emails.
                                </div>
                            )}
                            {accounts.map((a) => (
                                <CandidateRow
                                    key={a.id}
                                    candidate={a}
                                    active={value === a.id}
                                    onPick={() => {
                                        onChange(a.id);
                                        setOpen(false);
                                    }}
                                />
                            ))}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>,
            document.body,
            )}
        </div>
    );
}

function CandidateRow({
    candidate: a,
    active,
    onPick,
}: {
    candidate: ComposeCandidate;
    active: boolean;
    onPick: () => void;
}) {
    const pct = a.daily_limit > 0 ? Math.min(100, Math.round((a.sent_today / a.daily_limit) * 100)) : 0;
    return (
        <button
            type="button"
            onClick={onPick}
            className={cn(
                "w-full px-3 py-2 flex items-start gap-2 text-left transition-colors hover:bg-slate-50",
                active && "bg-sky-50/60",
            )}
        >
            <span
                className={cn(
                    "size-1.5 rounded-full shrink-0 mt-[7px]",
                    a.auth_state === "failing" ? "bg-rose-500" : a.auth_state === "passing" ? "bg-emerald-500" : "bg-slate-300",
                )}
                title={
                    a.auth_state === "failing"
                        ? "Domain authentication failing (SPF/DMARC)"
                        : a.auth_state === "passing"
                          ? "Domain authenticated"
                          : "Domain authentication not checked yet"
                }
            />
            <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5 min-w-0">
                    <span className="text-[12px] font-medium text-slate-900 truncate">
                        {a.name || a.email}
                    </span>
                    {a.recommended && (
                        <span className="inline-flex items-center gap-0.5 h-4 px-1 rounded bg-sky-50 text-sky-700 text-[9.5px] font-semibold uppercase tracking-wide shrink-0">
                            best
                        </span>
                    )}
                    {active && <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />}
                </span>
                <span className="block font-mono text-[10.5px] text-slate-500 truncate">{a.email}</span>
                <span className="mt-1 flex items-center gap-2">
                    <span className="w-16 h-1 rounded-full bg-slate-100 overflow-hidden shrink-0">
                        <span
                            className={cn(
                                "block h-full rounded-full",
                                pct >= 100 ? "bg-rose-400" : pct >= 80 ? "bg-amber-400" : "bg-sky-500",
                            )}
                            style={{ width: `${pct}%` }}
                        />
                    </span>
                    <span className="text-[10px] text-slate-400 tabular-nums shrink-0">
                        {a.sent_today}/{a.daily_limit} today
                    </span>
                    {a.history_messages > 0 && (
                        <span
                            className="inline-flex items-center gap-0.5 text-[10px] text-emerald-700 shrink-0"
                            title="Messages already exchanged with this recipient from this mailbox"
                        >
                            <MessagesSquareIcon className="w-2.5 h-2.5" />
                            {a.history_messages}
                        </span>
                    )}
                    {a.auth_state === "failing" && (
                        <span className="inline-flex items-center gap-0.5 text-[10px] text-rose-600 shrink-0">
                            <ShieldAlertIcon className="w-2.5 h-2.5" />
                            auth
                        </span>
                    )}
                </span>
            </span>
        </button>
    );
}
