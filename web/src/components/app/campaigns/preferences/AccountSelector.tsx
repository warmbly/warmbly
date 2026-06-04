// AccountSelector — specific-mailbox multi-select for the explicit sender
// strategy. Mirrors the contacts CategoryPicker / mailbox TagSelector visual
// language (bordered chip box + framer-motion dropdown with a search header +
// checkbox-square rows), so account, tag and category pickers look identical
// across the app.
//
// value is a string[] of email_account_ids; onChange returns the next list.
// Mailboxes are fed by useEmails with an empty query/tag and a large limit so
// the whole pool is selectable from one dropdown.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, PlusIcon, XIcon, MailIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";

// A health dot color from the mailbox status, mirroring InboxDetails.statusTone.
function healthDot(status: string): string {
    const s = status?.toLowerCase();
    if (s === "active" || s === "healthy") return "bg-emerald-500";
    if (s === "warming" || s === "warning") return "bg-amber-500";
    if (s === "revoked" || s === "error" || s === "inactive") return "bg-rose-500";
    return "bg-slate-300";
}

export default function AccountSelector({
    value,
    onChange,
}: {
    value: string[];
    onChange: (next: string[]) => void;
}) {
    // Pull the whole pool in one shot (empty query/tag, large limit).
    const { emails, isLoading } = useEmails({ query: "", tag: "", limit: 200 });

    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    const placement = useFlipPlacement(triggerRef, open, 270);

    const byId = React.useMemo(() => {
        const m = new Map<string, (typeof emails)[number]>();
        for (const e of emails) m.set(e.id, e);
        return m;
    }, [emails]);

    // Selected chips, even if the mailbox isn't in the current page yet keep the
    // id so a stale/unknown selection still renders and can be removed.
    const selectedChips = value.map((id) => ({ id, inbox: byId.get(id) }));

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return emails;
        return emails.filter(
            (e) =>
                e.email.toLowerCase().includes(q) ||
                (e.name ?? "").toLowerCase().includes(q) ||
                (e.provider ?? "").toLowerCase().includes(q),
        );
    }, [emails, query]);

    function toggle(id: string) {
        if (value.includes(id)) onChange(value.filter((v) => v !== id));
        else onChange([...value, id]);
    }

    return (
        <div ref={ref} className="relative">
            <div ref={triggerRef} className="rounded-md border border-slate-200 bg-white min-h-[34px]">
                {selectedChips.length === 0 ? (
                    <div
                        onClick={() => setOpen((o) => !o)}
                        className="px-3 py-2 text-[11.5px] text-slate-400 cursor-pointer hover:text-slate-600"
                    >
                        Click to add specific accounts…
                    </div>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {selectedChips.map(({ id, inbox }) => (
                            <span
                                key={id}
                                className="inline-flex items-center gap-1.5 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium border border-slate-200 bg-slate-50 text-slate-700"
                            >
                                <span className={`size-2 rounded-full shrink-0 ${healthDot(inbox?.status ?? "")}`} />
                                <span className="truncate max-w-[200px]">{inbox?.email ?? id}</span>
                                <button
                                    type="button"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        toggle(id);
                                    }}
                                    className="opacity-70 hover:opacity-100"
                                    aria-label={`Remove ${inbox?.email ?? id}`}
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                        <button
                            type="button"
                            onClick={() => setOpen((o) => !o)}
                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700"
                        >
                            <PlusIcon className="w-2.5 h-2.5" />
                            Add
                        </button>
                    </div>
                )}
            </div>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={`absolute left-0 right-0 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden ${
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1"
                        }`}
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                placeholder="Search accounts…"
                                autoFocus
                                className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            {isLoading && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">Loading…</div>
                            )}
                            {!isLoading && filtered.length === 0 && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    {emails.length === 0 ? "No mailboxes connected yet." : "No matches."}
                                </div>
                            )}
                            {filtered.map((e) => {
                                const checked = value.includes(e.id);
                                return (
                                    <button
                                        key={e.id}
                                        type="button"
                                        onClick={() => toggle(e.id)}
                                        className="w-full px-2.5 h-9 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                    >
                                        <span
                                            className={`size-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                checked ? "border-slate-900 bg-slate-900" : "border-slate-300 bg-white"
                                            }`}
                                        >
                                            {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                        </span>
                                        <span className={`size-2 rounded-full shrink-0 ${healthDot(e.status)}`} />
                                        <span className="min-w-0 flex-1 flex flex-col items-start leading-tight">
                                            <span className="truncate w-full">{e.email}</span>
                                            {e.provider && (
                                                <span className="text-[10px] text-slate-400 uppercase tracking-[0.08em]">
                                                    {e.provider}
                                                </span>
                                            )}
                                        </span>
                                    </button>
                                );
                            })}
                        </div>
                        <div className="px-2.5 h-7 flex items-center gap-1.5 text-[11px] text-slate-400 border-t border-slate-100">
                            <MailIcon className="w-3 h-3" />
                            {value.length} selected
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
