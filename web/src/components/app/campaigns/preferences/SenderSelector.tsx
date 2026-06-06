// SenderSelector — the single, unified sending-account picker. One dropdown
// holds BOTH mailbox tags and individual mailboxes; you can mix them freely.
// There is no by-tag / by-account switcher anymore: selecting nothing means
// "all active mailboxes", a tag means "every mailbox in that tag", and a
// mailbox means "exactly that mailbox". The resolved pool on the backend is the
// union of the picked tags and the picked mailboxes (and falls back to all
// active mailboxes when nothing is picked).
//
// Visual language mirrors the contacts CategoryPicker / mailbox TagSelector:
// a bordered chip box + framer-motion dropdown with a search header and
// checkbox-square rows, grouped into Tags and Mailboxes sections.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, PlusIcon, XIcon, MailIcon, TagIcon } from "lucide-react";
import { useUserProfile } from "@/hooks/context/user";
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

// hexToRgba converts "#rrggbb" to "rgba(...)"; non-hex falls back to a slate
// tint so the chip stays visible.
function hexToRgba(hex: string, alpha: number): string {
    const m = /^#([0-9a-f]{6})$/i.exec(hex);
    if (!m) return `rgba(100,116,139,${alpha})`;
    const v = m[1];
    return `rgba(${parseInt(v.slice(0, 2), 16)},${parseInt(v.slice(2, 4), 16)},${parseInt(v.slice(4, 6), 16)},${alpha})`;
}

export default function SenderSelector({
    selectedTags,
    onTagsChange,
    selectedAccounts,
    onAccountsChange,
}: {
    selectedTags: string[];
    onTagsChange: (next: string[]) => void;
    selectedAccounts: string[];
    onAccountsChange: (next: string[]) => void;
}) {
    const profile = useUserProfile();
    const tags = React.useMemo(
        () => [...(profile?.user.tags ?? [])].sort((a, b) => a.position - b.position),
        [profile?.user.tags],
    );
    const { emails, isLoading } = useEmails({ query: "", tag: "", limit: 200 });

    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    const placement = useFlipPlacement(triggerRef, open, 320);

    const tagById = React.useMemo(() => {
        const m = new Map<string, (typeof tags)[number]>();
        for (const t of tags) m.set(t.id, t);
        return m;
    }, [tags]);
    const mailboxById = React.useMemo(() => {
        const m = new Map<string, (typeof emails)[number]>();
        for (const e of emails) m.set(e.id, e);
        return m;
    }, [emails]);

    const tagChips = selectedTags
        .map((id) => tagById.get(id))
        .filter((t): t is NonNullable<typeof t> => !!t);
    // Keep unknown/stale account ids around so a selection still renders + can be removed.
    const accountChips = selectedAccounts.map((id) => ({ id, inbox: mailboxById.get(id) }));

    const q = query.trim().toLowerCase();
    const filteredTags = React.useMemo(
        () => (!q ? tags : tags.filter((t) => t.title.toLowerCase().includes(q))),
        [tags, q],
    );
    const filteredMailboxes = React.useMemo(
        () =>
            !q
                ? emails
                : emails.filter(
                      (e) =>
                          e.email.toLowerCase().includes(q) ||
                          (e.name ?? "").toLowerCase().includes(q) ||
                          (e.provider ?? "").toLowerCase().includes(q),
                  ),
        [emails, q],
    );

    function toggleTag(id: string) {
        if (selectedTags.includes(id)) onTagsChange(selectedTags.filter((v) => v !== id));
        else onTagsChange([...selectedTags, id]);
    }
    function toggleAccount(id: string) {
        if (selectedAccounts.includes(id)) onAccountsChange(selectedAccounts.filter((v) => v !== id));
        else onAccountsChange([...selectedAccounts, id]);
    }

    const totalSelected = selectedTags.length + selectedAccounts.length;
    const hasChips = tagChips.length > 0 || accountChips.length > 0;

    return (
        <div ref={ref} className="relative">
            <div ref={triggerRef} className="rounded-md border border-slate-200 bg-white min-h-[34px]">
                {!hasChips ? (
                    <div
                        onClick={() => setOpen((o) => !o)}
                        className="px-3 py-2 text-[11.5px] text-slate-400 cursor-pointer hover:text-slate-600"
                    >
                        All active mailboxes — click to narrow by tag or pick specific ones…
                    </div>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {tagChips.map((t) => (
                            <span
                                key={`tag-${t.id}`}
                                className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium"
                                style={{
                                    backgroundColor: hexToRgba(t.color, 0.12),
                                    color: t.color,
                                    border: `1px solid ${hexToRgba(t.color, 0.25)}`,
                                }}
                            >
                                <TagIcon className="w-2.5 h-2.5 shrink-0" />
                                <span className="truncate max-w-[160px]">{t.title}</span>
                                <button
                                    type="button"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        toggleTag(t.id);
                                    }}
                                    className="opacity-70 hover:opacity-100"
                                    aria-label={`Remove ${t.title}`}
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                        {accountChips.map(({ id, inbox }) => (
                            <span
                                key={`acct-${id}`}
                                className="inline-flex items-center gap-1.5 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium border border-slate-200 bg-slate-50 text-slate-700"
                            >
                                <span className={`size-2 rounded-full shrink-0 ${healthDot(inbox?.status ?? "")}`} />
                                <span className="truncate max-w-[200px]">{inbox?.email ?? id}</span>
                                <button
                                    type="button"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        toggleAccount(id);
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
                                placeholder="Search tags or mailboxes…"
                                autoFocus
                                className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-64 overflow-y-auto py-1">
                            {/* Tags */}
                            {filteredTags.length > 0 && (
                                <>
                                    <div className="px-2.5 pt-1.5 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                        Tags
                                    </div>
                                    {filteredTags.map((t) => {
                                        const checked = selectedTags.includes(t.id);
                                        return (
                                            <button
                                                key={t.id}
                                                type="button"
                                                onClick={() => toggleTag(t.id)}
                                                className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                            >
                                                <span
                                                    className={`size-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                        checked
                                                            ? "border-slate-900 bg-slate-900"
                                                            : "border-slate-300 bg-white"
                                                    }`}
                                                >
                                                    {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                                </span>
                                                <span
                                                    className="size-2.5 rounded-full shrink-0"
                                                    style={{ backgroundColor: t.color }}
                                                />
                                                <span className="truncate">{t.title}</span>
                                            </button>
                                        );
                                    })}
                                </>
                            )}

                            {/* Mailboxes */}
                            <div className="px-2.5 pt-2 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                Mailboxes
                            </div>
                            {isLoading && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">Loading…</div>
                            )}
                            {!isLoading && filteredMailboxes.length === 0 && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    {emails.length === 0 ? "No mailboxes connected yet." : "No matches."}
                                </div>
                            )}
                            {filteredMailboxes.map((e) => {
                                const checked = selectedAccounts.includes(e.id);
                                return (
                                    <button
                                        key={e.id}
                                        type="button"
                                        onClick={() => toggleAccount(e.id)}
                                        className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                    >
                                        <span
                                            className={`size-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                checked ? "border-slate-900 bg-slate-900" : "border-slate-300 bg-white"
                                            }`}
                                        >
                                            {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                        </span>
                                        <span className={`size-2.5 rounded-full shrink-0 ${healthDot(e.status)}`} />
                                        <span className="min-w-0 flex-1 truncate text-left">{e.email}</span>
                                        {e.provider && (
                                            <span className="shrink-0 max-w-[88px] truncate pl-2 text-[10px] capitalize tracking-[0.06em] text-slate-400">
                                                {e.provider}
                                            </span>
                                        )}
                                    </button>
                                );
                            })}
                        </div>
                        <div className="px-2.5 h-7 flex items-center gap-1.5 text-[11px] text-slate-400 border-t border-slate-100">
                            <MailIcon className="w-3 h-3" />
                            {totalSelected === 0
                                ? "Nothing selected — all active mailboxes"
                                : `${selectedTags.length} tag${selectedTags.length === 1 ? "" : "s"} · ${selectedAccounts.length} mailbox${selectedAccounts.length === 1 ? "" : "es"}`}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
