// Add existing contacts to a campaign as leads.
//
// The Leads tab could import a file, sync a sheet, or type a new contact, but
// had no way to pull in people already in the workspace. This dialog searches
// the contact list (query + categories), shows who is already a lead, and
// attaches the selection through the bulk contact update (add_campaigns), the
// same path the import wizard uses. "Select all matching" pages through the
// search up to the bulk-update cap so a whole category can be added in one go.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import {
    AlertCircleIcon,
    CheckIcon,
    Loader2Icon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { SearchInput } from "@/components/ui/field";
import CategoryPicker from "./CategoryPicker";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import useUpdateContactsBulk from "@/lib/api/hooks/app/contacts/useUpdateContactsBulk";
import searchContacts from "@/lib/api/client/app/contacts/searchContacts";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn, hexToRgba } from "@/lib/utils";

// Backend caps: 100 rows per search page, 1000 contacts per bulk update.
const PAGE = 100;
const MAX_SELECTION = 1000;

interface Props {
    open: boolean;
    onClose: () => void;
    campaign: MiniCampaign;
}

function displayName(c: Contact): string {
    const n = `${c.first_name ?? ""} ${c.last_name ?? ""}`.trim();
    return n || c.email;
}

export default function AddFromContactsDialog({ open, onClose, campaign }: Props) {
    const queryClient = useQueryClient();
    const bulk = useUpdateContactsBulk();

    const [query, setQuery] = React.useState("");
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [selected, setSelected] = React.useState<Set<string>>(() => new Set());
    const [selectingAll, setSelectingAll] = React.useState(false);
    const [capped, setCapped] = React.useState(false);

    // Debounce the query so a fast typist does not fire a search per keystroke.
    const [debounced, setDebounced] = React.useState("");
    React.useEffect(() => {
        const t = setTimeout(() => setDebounced(query.trim()), 200);
        return () => clearTimeout(t);
    }, [query]);

    const options = React.useMemo<SearchContacts>(
        () => ({
            query: debounced,
            filters: [],
            campaign_ids: [],
            category_ids: categoryIds.length > 0 ? categoryIds : undefined,
            sort_by: "created_at",
            reverse: false,
        }),
        [debounced, categoryIds],
    );

    const search = useSearchContacts({ options, limit: PAGE, enabled: open, keepPrevious: true });
    const contacts = React.useMemo(() => search.contacts ?? [], [search.contacts]);

    React.useEffect(() => {
        if (!open) {
            setQuery("");
            setDebounced("");
            setCategoryIds([]);
            setSelected(new Set());
            setSelectingAll(false);
            setCapped(false);
        }
    }, [open]);

    const inCampaign = React.useCallback(
        (c: Contact) => (c.campaigns ?? []).some((x) => x.id === campaign.id),
        [campaign.id],
    );

    const selectable = React.useMemo(() => contacts.filter((c) => !inCampaign(c)), [contacts, inCampaign]);
    const allLoadedSelected = selectable.length > 0 && selectable.every((c) => selected.has(c.id));

    const toggle = (id: string) =>
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else if (next.size < MAX_SELECTION) next.add(id);
            return next;
        });

    const toggleLoaded = () =>
        setSelected((prev) => {
            const next = new Set(prev);
            if (allLoadedSelected) selectable.forEach((c) => next.delete(c.id));
            else for (const c of selectable) if (next.size < MAX_SELECTION) next.add(c.id);
            return next;
        });

    // Walk every page of the current search and select what is not already a
    // lead, stopping at the bulk cap so the request that follows cannot fail.
    async function selectAllMatching() {
        if (selectingAll) return;
        setSelectingAll(true);
        setCapped(false);
        try {
            const next = new Set(selected);
            let cursor: string | null = null;
            let hitCap = false;
            for (;;) {
                const page = await searchContacts(options, cursor, PAGE);
                for (const c of page.data ?? []) {
                    if (inCampaign(c)) continue;
                    if (next.size >= MAX_SELECTION) {
                        hitCap = true;
                        break;
                    }
                    next.add(c.id);
                }
                if (hitCap || !page.pagination.has_more || !page.pagination.next_cursor) break;
                cursor = page.pagination.next_cursor;
            }
            setSelected(next);
            setCapped(hitCap);
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setSelectingAll(false);
        }
    }

    async function submit() {
        if (bulk.isPending || selected.size === 0) return;
        try {
            await bulk.mutateAsync({
                contacts: [...selected],
                add_campaigns: [campaign.id],
                remove_campaigns: [],
                fields: [],
            });
            // The Leads tab is a contacts search scoped to this campaign.
            await queryClient.invalidateQueries({ queryKey: ["contacts"] });
            toast.success(`Added ${selected.size} lead${selected.size === 1 ? "" : "s"} to ${campaign.name}`);
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    const busy = bulk.isPending;
    const requestClose = React.useCallback(() => {
        if (!busy) onClose();
    }, [busy, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // An open picker or the confirm dialog owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    const total = search.data?.pages?.[0]?.pagination.total ?? null;

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Add leads from contacts"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[720px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <UsersIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Add leads
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">From contacts</span>
                            <span className="hidden sm:inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium max-w-[200px] truncate">
                                → {campaign.name}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <div className="px-4 py-3 border-b border-slate-100 flex flex-col sm:flex-row sm:items-center gap-2 shrink-0">
                            <SearchInput
                                value={query}
                                onChange={setQuery}
                                placeholder="Search name, email, company…"
                                autoFocus
                                className="w-full sm:w-64"
                            />
                            <div className="flex-1 min-w-0">
                                <CategoryPicker
                                    value={categoryIds}
                                    onChange={setCategoryIds}
                                    placeholder="Filter by category…"
                                    allowCreate={false}
                                />
                            </div>
                        </div>

                        <div className="px-4 h-8 flex items-center gap-3 border-b border-slate-100 text-[11px] text-slate-500 shrink-0">
                            <button
                                type="button"
                                onClick={toggleLoaded}
                                disabled={selectable.length === 0}
                                className="inline-flex items-center gap-1.5 hover:text-slate-900 disabled:opacity-50 transition-colors"
                            >
                                <CheckSquare checked={allLoadedSelected} />
                                {allLoadedSelected ? "Clear loaded" : `Select loaded (${selectable.length})`}
                            </button>
                            {(search.hasNextPage || contacts.length >= PAGE) && (
                                <button
                                    type="button"
                                    onClick={selectAllMatching}
                                    disabled={selectingAll}
                                    className="inline-flex items-center gap-1 hover:text-slate-900 disabled:opacity-50 transition-colors"
                                >
                                    {selectingAll && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    Select all matching{total != null ? ` (${total.toLocaleString()})` : ""}
                                </button>
                            )}
                            <span className="ml-auto tabular-nums">
                                {selected.size.toLocaleString()} selected
                            </span>
                        </div>

                        <div className="flex-1 min-h-[280px] overflow-y-auto">
                            {search.isPending ? (
                                <div className="p-3 space-y-1.5">
                                    {[...Array(6)].map((_, i) => (
                                        <div key={i} className="h-9 rounded-md bg-slate-100 animate-pulse" />
                                    ))}
                                </div>
                            ) : search.isError ? (
                                <div className="px-5 py-10 text-center">
                                    <AlertCircleIcon className="w-4 h-4 text-rose-500 mx-auto mb-2" />
                                    <p className="text-[12.5px] text-slate-900 font-medium">Couldn't load contacts</p>
                                    <button
                                        type="button"
                                        onClick={() => search.refetch()}
                                        className="mt-2 h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                    >
                                        Retry
                                    </button>
                                </div>
                            ) : contacts.length === 0 ? (
                                <div className="px-5 py-10 text-center">
                                    <p className="text-[12.5px] text-slate-900 font-medium">
                                        {debounced || categoryIds.length > 0 ? "No contacts match" : "No contacts yet"}
                                    </p>
                                    <p className="text-[11.5px] text-slate-400 mt-0.5">
                                        {debounced || categoryIds.length > 0
                                            ? "Try a different search or category."
                                            : "Import a file or add contacts first."}
                                    </p>
                                </div>
                            ) : (
                                <ul className="divide-y divide-slate-100">
                                    {contacts.map((c) => {
                                        const already = inCampaign(c);
                                        const on = selected.has(c.id);
                                        return (
                                            <li key={c.id}>
                                                <button
                                                    type="button"
                                                    disabled={already}
                                                    onClick={() => toggle(c.id)}
                                                    aria-pressed={on}
                                                    className={cn(
                                                        "w-full px-4 h-11 flex items-center gap-3 text-left transition-colors",
                                                        already
                                                            ? "cursor-default"
                                                            : on
                                                              ? "bg-sky-50/60 hover:bg-sky-50"
                                                              : "hover:bg-slate-50",
                                                    )}
                                                >
                                                    <CheckSquare checked={on} muted={already} />
                                                    <div className="min-w-0 flex-1">
                                                        <div className="flex items-center gap-2 min-w-0">
                                                            <span
                                                                className={cn(
                                                                    "text-[12.5px] font-medium truncate",
                                                                    already ? "text-slate-400" : "text-slate-900",
                                                                )}
                                                            >
                                                                {displayName(c)}
                                                            </span>
                                                            {c.company && (
                                                                <span className="text-[11px] text-slate-400 truncate hidden sm:inline">
                                                                    {c.company}
                                                                </span>
                                                            )}
                                                        </div>
                                                        <div className={cn("text-[11px] truncate", already ? "text-slate-300" : "text-slate-500")}>
                                                            {c.email}
                                                        </div>
                                                    </div>
                                                    <div className="hidden md:flex items-center gap-1 shrink-0 max-w-[220px] overflow-hidden">
                                                        {(c.categories ?? []).slice(0, 3).map((cat) => (
                                                            <span
                                                                key={cat.id}
                                                                className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10.5px] font-medium truncate"
                                                                style={{
                                                                    backgroundColor: hexToRgba(cat.color, 0.12),
                                                                    color: cat.color,
                                                                }}
                                                            >
                                                                <span className="size-1.5 rounded-full shrink-0" style={{ backgroundColor: cat.color }} />
                                                                {cat.title}
                                                            </span>
                                                        ))}
                                                    </div>
                                                    {already && (
                                                        <span className="shrink-0 inline-flex items-center gap-1 h-5 px-1.5 rounded bg-emerald-50 text-emerald-700 text-[10px] font-medium">
                                                            <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} />
                                                            Lead
                                                        </span>
                                                    )}
                                                </button>
                                            </li>
                                        );
                                    })}
                                    {search.hasNextPage && (
                                        <li className="p-2">
                                            <button
                                                type="button"
                                                onClick={() => search.fetchNextPage()}
                                                disabled={search.isFetchingNextPage}
                                                className="w-full h-8 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-50 inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60"
                                            >
                                                {search.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                                Load more
                                            </button>
                                        </li>
                                    )}
                                </ul>
                            )}
                        </div>

                        <footer className="px-3 min-h-12 py-1.5 sm:py-0 sm:h-12 border-t border-slate-200 flex items-center gap-2 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 min-w-0 truncate">
                                {capped
                                    ? `Capped at ${MAX_SELECTION.toLocaleString()} per batch. Add these, then repeat for the rest.`
                                    : "Contacts already in this campaign are skipped."}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={busy}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={busy || selected.size === 0}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <UsersIcon className="w-3 h-3" />}
                                Add {selected.size > 0 ? selected.size.toLocaleString() : ""} lead{selected.size === 1 ? "" : "s"}
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function CheckSquare({ checked, muted }: { checked: boolean; muted?: boolean }) {
    return (
        <span
            aria-hidden="true"
            className={cn(
                "size-3.5 rounded border flex items-center justify-center transition-colors shrink-0",
                muted
                    ? "border-slate-200 bg-slate-100"
                    : checked
                      ? "border-sky-600 bg-sky-600"
                      : "border-slate-300 bg-white",
            )}
        >
            {checked && !muted && <CheckIcon className="w-2 h-2 text-white" strokeWidth={3} />}
        </span>
    );
}
