// Advanced filter sheet for the unibox.
//
// Mirrors the params that GET /unibox actually supports today:
//   - free text (subject ILIKE)
//   - from
//   - account (one of the user's email_accounts)
//   - unseen only
//   - since / until date range
//   - sort: newest / oldest
//
// Sheet pattern matches ContactFilters (slim right-side panel,
// sticky header + footer, draft state mirrors parent until Apply
// so we don't refetch while the user is mid-build).

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    Loader2Icon,
    MailIcon,
    RotateCcwIcon,
    SearchIcon,
    TagIcon,
    XIcon,
} from "lucide-react";
import { SearchInput, TextInput } from "@/components/ui/field";
import { SectionBar } from "@/components/layout/Page";
import { useAppStore } from "@/stores";
import { useUserProfile } from "@/hooks/context/user";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

interface Props {
    open: boolean;
    setOpen: (o: boolean) => void;
    filters: UniboxSearchParams;
    setFilters: React.Dispatch<React.SetStateAction<UniboxSearchParams>>;
    loading?: boolean;
}

export function UniboxFilterSheet({ open, setOpen, filters, setFilters, loading }: Props) {
    const [draft, setDraft] = React.useState<UniboxSearchParams>(filters);
    const emails = useAppStore((s) => s.emails);
    const p = useUserProfile();
    const tags = p.user.tags ?? [];

    React.useEffect(() => {
        if (open) setDraft(filters);
    }, [open, filters]);

    const accountIds = React.useMemo(() => new Set(draft.accountIds ?? []), [draft.accountIds]);
    const selectedTagId = draft.tagId;

    // Accounts that carry the currently-selected tag. Used to compute
    // whether the chip should show as "all picked" and to resolve to
    // concrete IDs at apply time.
    const accountsByTag = React.useMemo(() => {
        if (!selectedTagId) return null;
        return emails.filter((e) => (e.tags ?? []).includes(selectedTagId));
    }, [emails, selectedTagId]);

    function toggleAccount(id: string) {
        setDraft((s) => {
            const next = new Set(s.accountIds ?? []);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return { ...s, accountIds: Array.from(next) };
        });
    }
    function selectTag(tagId: string) {
        setDraft((s) => {
            if (s.tagId === tagId) {
                // Clicking the active tag clears it.
                return { ...s, tagId: undefined };
            }
            return { ...s, tagId };
        });
    }
    function selectAllAccounts() {
        setDraft((s) => ({ ...s, accountIds: emails.map((e) => e.id), tagId: undefined }));
    }
    function clearAccounts() {
        setDraft((s) => ({ ...s, accountIds: [], tagId: undefined }));
    }

    const activeCount = countActive(draft);

    const apply = () => {
        // If a tag is selected, resolve it to the underlying account
        // IDs before committing — the server only knows about
        // accountIds. We keep tagId in the draft for UI display but
        // strip it before passing up.
        let resolvedIds = draft.accountIds ?? [];
        if (draft.tagId) {
            const tagAccountIds = emails
                .filter((e) => (e.tags ?? []).includes(draft.tagId!))
                .map((e) => e.id);
            // Tag selection replaces manual picks — simpler mental model.
            resolvedIds = tagAccountIds;
        }
        setFilters({ ...draft, accountIds: resolvedIds });
        setOpen(false);
    };

    const reset = () => {
        setDraft({ sortBy: "newest" });
    };

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.18 }}
                    onClick={() => setOpen(false)}
                    className="fixed inset-0 z-[100] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
                >
                    <motion.aside
                        key="panel"
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", stiffness: 300, damping: 32 }}
                        onClick={(e) => e.stopPropagation()}
                        className="flex flex-col bg-white w-[420px] max-w-[95%] h-full border-l border-slate-200 shadow-[-8px_0_24px_-12px_rgba(15,23,42,0.12)]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-3 shrink-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Filters
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-700">
                                {activeCount === 0
                                    ? "No filters applied"
                                    : `${activeCount} ${activeCount === 1 ? "filter" : "filters"} active`}
                            </span>
                            <button
                                type="button"
                                onClick={() => setOpen(false)}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="flex-1 min-h-0 overflow-y-auto">
                            <SectionBar label="Search" />
                            <div className="px-4 py-3 space-y-2">
                                <SearchInput
                                    value={draft.query ?? ""}
                                    onChange={(v) => setDraft((s) => ({ ...s, query: v || undefined }))}
                                    placeholder="Subject, snippet, contents…"
                                />
                            </div>

                            <SectionBar label="Sender" />
                            <div className="px-4 py-3 space-y-2">
                                <TextInput
                                    value={draft.from ?? ""}
                                    onChange={(v) => setDraft((s) => ({ ...s, from: v || undefined }))}
                                    placeholder="name@company.com or substring"
                                    className="w-full"
                                />
                            </div>

                            <SectionBar
                                label="Accounts"
                                count={
                                    selectedTagId
                                        ? accountsByTag?.length
                                        : draft.accountIds?.length || undefined
                                }
                            >
                                {(draft.accountIds?.length || draft.tagId) ? (
                                    <button
                                        type="button"
                                        onClick={clearAccounts}
                                        className="text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                                    >
                                        Clear
                                    </button>
                                ) : (
                                    <button
                                        type="button"
                                        onClick={selectAllAccounts}
                                        className="text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                                    >
                                        Select all
                                    </button>
                                )}
                            </SectionBar>

                            {/* Tag chips — fastest way to scope to a group of mailboxes.
                                Picking a tag visually expands the selection: every
                                account that carries the tag is included at Apply time. */}
                            {tags.length > 0 && (
                                <div className="px-4 pt-3">
                                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                                        <TagIcon className="w-3 h-3" />
                                        Tags
                                    </div>
                                    <div className="flex flex-wrap gap-1.5">
                                        {tags.map((t) => {
                                            const active = selectedTagId === t.id;
                                            const count = emails.filter((e) => (e.tags ?? []).includes(t.id)).length;
                                            return (
                                                <button
                                                    key={t.id}
                                                    type="button"
                                                    onClick={() => selectTag(t.id)}
                                                    className={`group h-6 pl-1.5 pr-2 rounded-full inline-flex items-center gap-1.5 text-[11.5px] font-medium border transition-colors ${
                                                        active
                                                            ? "bg-slate-900 text-white border-slate-900"
                                                            : "bg-white text-slate-700 border-slate-200 hover:border-slate-300"
                                                    }`}
                                                >
                                                    <span
                                                        aria-hidden
                                                        className="size-2 rounded-full"
                                                        style={{ backgroundColor: t.color }}
                                                    />
                                                    <span className="truncate max-w-[120px]">{t.title}</span>
                                                    <span
                                                        className={`font-mono tabular-nums text-[10px] ${
                                                            active ? "text-white/80" : "text-slate-400"
                                                        }`}
                                                    >
                                                        {count}
                                                    </span>
                                                </button>
                                            );
                                        })}
                                    </div>
                                </div>
                            )}

                            {/* Individual account chips with avatars. Multi-select.
                                When a tag is active, accounts that belong to it get
                                a subtle ring so the relationship is visible. */}
                            <div className="px-4 py-3">
                                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                                    <MailIcon className="w-3 h-3" />
                                    Mailboxes
                                </div>
                                {emails.length === 0 ? (
                                    <p className="text-[11.5px] text-slate-400 py-2">
                                        No mailboxes connected yet.
                                    </p>
                                ) : (
                                    <div className="space-y-1">
                                        {emails.map((e) => {
                                            const checked = accountIds.has(e.id);
                                            const tagMatch =
                                                selectedTagId &&
                                                (e.tags ?? []).includes(selectedTagId);
                                            return (
                                                <button
                                                    key={e.id}
                                                    type="button"
                                                    onClick={() => toggleAccount(e.id)}
                                                    className={`w-full flex items-center gap-2.5 px-2 py-1.5 rounded-md transition-colors text-left ${
                                                        checked
                                                            ? "bg-sky-50/80 hover:bg-sky-50"
                                                            : tagMatch
                                                                ? "bg-slate-50 hover:bg-slate-100"
                                                                : "hover:bg-slate-50"
                                                    }`}
                                                >
                                                    <span
                                                        className={`size-4 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                            checked
                                                                ? "bg-slate-900 border-slate-900 text-white"
                                                                : "bg-white border-slate-300"
                                                        }`}
                                                    >
                                                        {checked && <CheckIcon className="w-2.5 h-2.5" />}
                                                    </span>
                                                    <span className="size-5 rounded-full bg-slate-100 text-slate-600 flex items-center justify-center text-[9px] font-semibold shrink-0">
                                                        {e.email.slice(0, 2).toUpperCase()}
                                                    </span>
                                                    <span className="text-[12px] text-slate-900 truncate flex-1">
                                                        {e.email}
                                                    </span>
                                                    {tagMatch && !checked && (
                                                        <span className="text-[10px] text-slate-400 font-mono">
                                                            via tag
                                                        </span>
                                                    )}
                                                </button>
                                            );
                                        })}
                                    </div>
                                )}
                            </div>

                            <SectionBar label="Status" />
                            <div className="px-4 py-3">
                                <Toggle3
                                    value={draft.unseen ?? undefined}
                                    onChange={(v) => setDraft((s) => ({ ...s, unseen: v }))}
                                    options={[
                                        { id: undefined, label: "Any" },
                                        { id: true, label: "Unread" },
                                        { id: false, label: "Read" },
                                    ]}
                                />
                            </div>

                            <SectionBar label="Dates" />
                            <div className="px-4 py-3 space-y-2">
                                <DateRow
                                    label="Since"
                                    value={draft.since}
                                    onChange={(v) => setDraft((s) => ({ ...s, since: v }))}
                                />
                                <DateRow
                                    label="Until"
                                    value={draft.until}
                                    onChange={(v) => setDraft((s) => ({ ...s, until: v }))}
                                />
                            </div>

                            <SectionBar label="Sort" />
                            <div className="px-4 py-3 space-y-2">
                                <Toggle3
                                    value={draft.sortBy ?? "newest"}
                                    onChange={(v) =>
                                        setDraft((s) => ({ ...s, sortBy: (v as "newest" | "oldest") ?? "newest" }))
                                    }
                                    options={[
                                        { id: "newest" as const, label: "Newest" },
                                        { id: "oldest" as const, label: "Oldest" },
                                    ]}
                                />
                            </div>
                        </div>

                        <div className="px-4 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0">
                            <button
                                type="button"
                                onClick={reset}
                                className="h-7 px-2.5 rounded-md text-[12px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                            >
                                <RotateCcwIcon className="w-3 h-3" />
                                Reset
                            </button>
                            <button
                                type="button"
                                onClick={() => setOpen(false)}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={apply}
                                disabled={loading}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {loading ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <SearchIcon className="w-3 h-3" />
                                )}
                                Apply
                            </button>
                        </div>
                    </motion.aside>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Toggle3<T extends string | boolean | undefined>({
    value,
    onChange,
    options,
}: {
    value: T;
    onChange: (v: T) => void;
    options: { id: T; label: string }[];
}) {
    return (
        <div className="inline-flex items-center rounded-md border border-slate-200 bg-white p-0.5">
            {options.map((o) => (
                <button
                    key={String(o.id)}
                    type="button"
                    onClick={() => onChange(o.id)}
                    className={`h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors ${
                        value === o.id ? "bg-slate-900 text-white" : "text-slate-500 hover:text-slate-900"
                    }`}
                >
                    {o.label}
                </button>
            ))}
        </div>
    );
}

function DateRow({
    label,
    value,
    onChange,
}: {
    label: string;
    value?: Date;
    onChange: (v: Date | undefined) => void;
}) {
    const enabled = value !== undefined;
    const dateStr = value ? toIsoDate(value) : "";
    return (
        <div className="flex items-center gap-2">
            <button
                type="button"
                onClick={() => onChange(enabled ? undefined : new Date())}
                className={`size-4 rounded border flex items-center justify-center transition-colors shrink-0 ${
                    enabled
                        ? "bg-slate-900 border-slate-900 text-white"
                        : "border-slate-300 hover:border-slate-400"
                }`}
                aria-pressed={enabled}
                aria-label={`Toggle ${label}`}
            >
                {enabled && (
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3">
                        <path d="M5 12l5 5L20 7" />
                    </svg>
                )}
            </button>
            <span className="text-[12px] text-slate-700 w-12 shrink-0">{label}</span>
            <input
                type="date"
                value={dateStr}
                onChange={(e) => {
                    const v = e.target.value;
                    if (!v) onChange(undefined);
                    else onChange(new Date(v));
                }}
                disabled={!enabled}
                className="flex-1 h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 disabled:bg-slate-50 disabled:text-slate-400 tabular-nums"
            />
        </div>
    );
}

function toIsoDate(d: Date): string {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
}

function countActive(f: UniboxSearchParams): number {
    let n = 0;
    if (f.query) n++;
    if (f.from) n++;
    if (f.tagId) n++;
    if (f.accountIds && f.accountIds.length > 0) n++;
    if (f.unseen !== undefined) n++;
    if (f.since) n++;
    if (f.until) n++;
    return n;
}
