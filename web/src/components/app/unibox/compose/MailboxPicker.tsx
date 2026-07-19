// MailboxPicker — the compose "From" selector. Defaults to Auto: the backend
// scores every active mailbox for the current recipient (conversation
// affinity, remaining daily budget, domain auth) and this control shows the
// resolved pick. The menu is a compact, searchable list (filterable by
// mailbox tag) so orgs with dozens of mailboxes can still pick in a second;
// per-row detail lives in the title tooltip instead of bloating the rows.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    MessagesSquareIcon,
    SearchIcon,
    SparklesIcon,
} from "lucide-react";
import type { ComposeCandidate, ComposeCandidatesResponse } from "@/lib/api/models/app/unibox/Compose";
import useClickOutside from "@/hooks/useClickOutside";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";

interface MailboxPickerProps {
    // "auto" or an account id.
    value: string;
    onChange: (next: string) => void;
    candidates: ComposeCandidatesResponse | undefined;
    // True while candidates refetch for a new recipient.
    loading?: boolean;
}

const PANEL_WIDTH = 300;

export default function MailboxPicker({ value, onChange, candidates, loading }: MailboxPickerProps) {
    const [open, setOpen] = React.useState(false);
    const [search, setSearch] = React.useState("");
    const [tagFilter, setTagFilter] = React.useState<string | null>(null);
    // Viewport anchor for the portaled panel (the compose window clips
    // overflow, so the menu can't render inside it).
    const [anchor, setAnchor] = React.useState<{ top: number; left: number; up: boolean } | null>(null);
    const boxRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(boxRef, () => setOpen(false));

    const storeEmails = useAppStore((s) => s.emails);
    const storeTags = useAppStore((s) => s.tags);

    const measure = React.useCallback(() => {
        const el = boxRef.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        // clientWidth excludes any scrollbar; innerWidth would let the
        // panel's right edge slide underneath it.
        const vw = document.documentElement.clientWidth;
        const vh = window.innerHeight;
        const left = Math.min(Math.max(r.left, 8), vw - PANEL_WIDTH - 8);
        // Flip above the trigger when the space below is tight.
        const up = vh - r.bottom < 300 && r.top > vh - r.bottom;
        setAnchor({ top: up ? r.top - 4 : r.bottom + 4, left, up });
    }, []);

    React.useEffect(() => {
        if (!open) {
            setSearch("");
            setTagFilter(null);
            return;
        }
        measure();
        window.addEventListener("scroll", measure, true);
        window.addEventListener("resize", measure);
        return () => {
            window.removeEventListener("scroll", measure, true);
            window.removeEventListener("resize", measure);
        };
    }, [open, measure]);

    const accounts = React.useMemo(() => candidates?.accounts ?? [], [candidates]);
    const recommended = accounts.find((a) => a.recommended) ?? accounts[0];
    const selected = value === "auto" ? recommended : accounts.find((a) => a.id === value);

    // Tag ids per account come from the store's full mailbox records; only
    // tags actually used by a listed account are offered as filters.
    const tagsByAccount = React.useMemo(() => {
        const m = new Map<string, string[]>();
        for (const e of storeEmails) m.set(e.id, e.tags ?? []);
        return m;
    }, [storeEmails]);

    // Every defined tag is offered (not just ones already in use) so a tag
    // created moments ago in the bulk bar is immediately selectable here;
    // filtering to an unused tag lands on the existing no-match state.
    const usedTags = React.useMemo(
        () => [...storeTags].sort((a, b) => a.position - b.position),
        [storeTags],
    );

    const filtered = React.useMemo(() => {
        const q = search.trim().toLowerCase();
        return accounts.filter((a) => {
            if (tagFilter && !(tagsByAccount.get(a.id) ?? []).includes(tagFilter)) return false;
            if (!q) return true;
            return `${a.email} ${a.name}`.toLowerCase().includes(q);
        });
    }, [accounts, search, tagFilter, tagsByAccount]);

    const pick = (next: string) => {
        onChange(next);
        setOpen(false);
    };

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
                            {/* Search + tag filter header */}
                            <div className="px-1.5 pt-1.5 pb-1 border-b border-slate-100">
                                <div className="flex items-center gap-1.5 px-1.5 h-6 rounded-md border border-slate-200 bg-white focus-within:border-sky-300 focus-within:ring-1 focus-within:ring-sky-100 transition-colors">
                                    <SearchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                    <input
                                        autoFocus
                                        value={search}
                                        onChange={(e) => setSearch(e.target.value)}
                                        placeholder="Search mailboxes…"
                                        className="flex-1 min-w-0 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                                    />
                                </div>
                                {usedTags.length > 0 && (
                                    <div className="mt-1 flex items-center gap-1 overflow-x-auto pb-0.5">
                                        {usedTags.map((t) => (
                                            <button
                                                key={t.id}
                                                type="button"
                                                onClick={() => setTagFilter((f) => (f === t.id ? null : t.id))}
                                                className={cn(
                                                    "h-5 px-1.5 rounded-full text-[10px] font-medium inline-flex items-center gap-1 shrink-0 border transition-colors",
                                                    tagFilter === t.id
                                                        ? "border-sky-300 bg-sky-50 text-sky-700"
                                                        : "border-slate-200 text-slate-500 hover:border-slate-300 hover:text-slate-700",
                                                )}
                                            >
                                                <span
                                                    className="size-1.5 rounded-full"
                                                    style={{ backgroundColor: t.color }}
                                                />
                                                {t.title}
                                            </button>
                                        ))}
                                    </div>
                                )}
                            </div>

                            {/* Auto */}
                            <button
                                type="button"
                                onClick={() => pick("auto")}
                                title={
                                    recommended
                                        ? `Picks ${recommended.email}${candidates?.recommended_reason ? `: ${candidates.recommended_reason}` : ""}`
                                        : "Picks the best mailbox for each recipient"
                                }
                                className={cn(
                                    "w-full h-8 px-2.5 flex items-center gap-2 text-left transition-colors hover:bg-slate-50",
                                    value === "auto" && "bg-sky-50/60",
                                )}
                            >
                                <SparklesIcon className="w-3 h-3 text-sky-500 shrink-0" />
                                <span className="text-[11.5px] font-medium text-slate-900">Auto</span>
                                <span className="min-w-0 flex-1 text-[10.5px] text-slate-400 truncate">
                                    {recommended ? recommended.email : "best mailbox per recipient"}
                                </span>
                                {value === "auto" && <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />}
                            </button>

                            {/* Candidates, best first */}
                            <div className="border-t border-slate-100 max-h-52 overflow-y-auto">
                                {filtered.length === 0 && (
                                    <div className="px-3 py-3 text-[11px] text-slate-400">
                                        {accounts.length === 0
                                            ? "No active mailboxes. Connect one under Emails."
                                            : "No mailboxes match."}
                                    </div>
                                )}
                                {filtered.map((a) => (
                                    <CandidateRow
                                        key={a.id}
                                        candidate={a}
                                        active={value === a.id}
                                        onPick={() => pick(a.id)}
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

// One compact line per mailbox: auth dot, address, then history + budget on
// the right. Everything else (name, score reasons) lives in the tooltip.
function CandidateRow({
    candidate: a,
    active,
    onPick,
}: {
    candidate: ComposeCandidate;
    active: boolean;
    onPick: () => void;
}) {
    const spent = a.daily_limit > 0 && a.sent_today >= a.daily_limit;
    return (
        <button
            type="button"
            onClick={onPick}
            title={[a.name, ...a.reasons].filter(Boolean).join(" · ")}
            className={cn(
                "w-full h-8 px-2.5 flex items-center gap-2 text-left transition-colors hover:bg-slate-50",
                active && "bg-sky-50/60",
            )}
        >
            <span
                className={cn(
                    "size-1.5 rounded-full shrink-0",
                    a.auth_state === "failing"
                        ? "bg-rose-500"
                        : a.auth_state === "passing"
                          ? "bg-emerald-500"
                          : "bg-slate-300",
                )}
            />
            <span className="min-w-0 flex-1 text-[11.5px] text-slate-800 truncate">
                {a.email}
                {a.recommended && (
                    <span className="ml-1.5 text-[9px] font-semibold uppercase tracking-wide text-sky-600">
                        best
                    </span>
                )}
            </span>
            {a.history_messages > 0 && (
                <span className="inline-flex items-center gap-0.5 text-[10px] text-emerald-700 shrink-0">
                    <MessagesSquareIcon className="w-2.5 h-2.5" />
                    {a.history_messages}
                </span>
            )}
            <span
                className={cn(
                    "font-mono text-[10px] tabular-nums shrink-0",
                    spent ? "text-rose-500" : "text-slate-400",
                )}
            >
                {a.sent_today}/{a.daily_limit}
            </span>
            {active && <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />}
        </button>
    );
}
