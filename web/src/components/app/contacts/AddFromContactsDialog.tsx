// Add existing contacts to a campaign as leads.
//
// The Leads tab could import a file, sync a sheet, or type a new contact, but
// had no way to pull in people already in the workspace. This dialog searches
// the contact list (query + labels), shows who is already a lead, and
// attaches the selection through the bulk contact update (add_campaigns), the
// same path the import wizard uses. "Select all matching" hands the server the
// search itself, so a whole category is one request.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseText, browseIds } from "@/lib/browse-contacts-campaigns";
import useBrowseDebouncedValue from "./filters/useBrowseDebouncedValue";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    CheckIcon,
    Loader2Icon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { SearchInput } from "@/components/ui/field";
import CategoryPicker from "./CategoryPicker";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import useUpdateContactsBulk from "@/lib/api/hooks/app/contacts/useUpdateContactsBulk";
import { useSetSegmentMembers } from "@/lib/api/hooks/app/segments";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn, hexToRgba, labelInk } from "@/lib/utils";
import type ContactSelection from "@/lib/api/models/app/contacts/ContactSelection";
import * as rowSelection from "./selection";
import type { RowSelection } from "./selection";

// Backend caps: 100 rows per search page, 10,000 contacts per explicit batch.
// "Select all matching" sends the filter instead, so it is not bound by the
// second one.
const PAGE = 100;
const MAX_SELECTION = 10_000;

// The target is either a campaign (contacts become leads) or a segment
// (contacts are pinned in as manual includes).
export type AddFromContactsTarget =
    | { kind: "campaign"; campaign: MiniCampaign }
    | { kind: "segment"; segment: { id: string; name: string } };

interface Props {
    open: boolean;
    onClose: () => void;
    campaign?: MiniCampaign;
    target?: AddFromContactsTarget;
}

function displayName(c: Contact): string {
    const n = `${c.first_name ?? ""} ${c.last_name ?? ""}`.trim();
    return n || c.email;
}

export default function AddFromContactsDialog({ open, onClose, campaign: campaignProp, target: targetProp }: Props) {
    const bulk = useUpdateContactsBulk();
    const members = useSetSegmentMembers();
    const target: AddFromContactsTarget = React.useMemo(
        () => targetProp ?? { kind: "campaign", campaign: campaignProp as MiniCampaign },
        [targetProp, campaignProp],
    );
    const targetName = target.kind === "campaign" ? target.campaign.name : target.segment.name;

    const browseName = `contacts:add-from:${target.kind}:${target.kind === "campaign" ? target.campaign.id : target.segment.id}`;
    const [query, setQuery] = useBrowseState(`${browseName}:query`, "", browseText);
    const [categoryIds, setCategoryIds] = useBrowseState<string[]>(`${browseName}:labels`, [], browseIds);
    // Ticked rows, or the search itself minus what was unticked after it; the
    // same two shapes the contacts table uses (./selection).
    const [rowSel, setRowSel] = React.useState<RowSelection>(rowSelection.emptySelection);

    // Debounce the query so a fast typist does not fire a search per keystroke.
    const debounced = useBrowseDebouncedValue(query.trim(), browseName);

    const options = React.useMemo<SearchContacts>(
        () => ({
            query: debounced,
            custom_field_filters: [],
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
            setRowSel(rowSelection.emptySelection);
        }
    }, [open]);

    // A changed search makes a select-all stale, so it drops back to nothing.
    React.useEffect(() => {
        setRowSel(rowSelection.emptySelection);
    }, [options]);

    const inCampaign = React.useCallback(
        (c: Contact) => target.kind === "campaign" && (c.campaigns ?? []).some((x) => x.id === target.campaign.id),
        [target],
    );

    // Contacts already in the campaign are shown but never selectable.
    const selectableIDs = React.useMemo(
        () => contacts.filter((c) => !inCampaign(c)).map((c) => c.id),
        [contacts, inCampaign],
    );
    const isSelected = React.useCallback((id: string) => rowSelection.isRowSelected(rowSel, id), [rowSel]);
    const allLoadedSelected = rowSelection.allLoadedSelected(rowSel, selectableIDs);

    // A ticked list still goes to the server as ids, so it keeps the batch cap;
    // "select all matching" does not, because it goes as the filter.
    const capped = (s: RowSelection) => (s.ids.length > MAX_SELECTION ? { ...s, ids: s.ids.slice(0, MAX_SELECTION) } : s);
    const toggle = (id: string) => setRowSel((s) => capped(rowSelection.toggleRow(s, id, !rowSelection.isRowSelected(s, id))));
    const toggleLoaded = () => setRowSel((s) => capped(rowSelection.toggleLoaded(s, selectableIDs)));

    // Hand the server the search rather than walking it here: one request, no
    // cap, and the set is resolved from the same filters the list ran.
    const selectAllMatching = () => setRowSel(rowSelection.selectAllMatching());

    async function submit() {
        if (busy || count === 0) return;
        try {
            if (target.kind === "campaign") {
                await bulk.mutateAsync({
                    ...selection,
                    add_campaigns: [target.campaign.id],
                    remove_campaigns: [],
                    fields: [],
                });
                toast.success(`Added ${count.toLocaleString()} lead${count === 1 ? "" : "s"} to ${target.campaign.name}`);
            } else {
                const added = await members.mutateAsync({ id: target.segment.id, selection, mode: "include" });
                toast.success(`Added ${added.toLocaleString()} contact${added === 1 ? "" : "s"} to ${target.segment.name}`);
            }
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    const busy = bulk.isPending || members.isPending;
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
    // What the Add button applies to, and how many contacts that is.
    const selection = React.useMemo<ContactSelection>(
        () => rowSelection.toRequest(rowSel, options),
        [rowSel, options],
    );
    const count = rowSelection.selectionCount(rowSel, total ?? 0);

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
                                {target.kind === "campaign" ? "Add leads" : "Add contacts"}
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">From contacts</span>
                            <span className="hidden sm:inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium max-w-[200px] truncate">
                                → {targetName}
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
                                    showMissing
                                    value={categoryIds}
                                    onChange={setCategoryIds}
                                    placeholder="Filter by label…"
                                    allowCreate={false}
                                />
                            </div>
                        </div>

                        <div className="px-4 h-8 flex items-center gap-3 border-b border-slate-100 text-[11px] text-slate-500 shrink-0">
                            <button
                                type="button"
                                onClick={toggleLoaded}
                                disabled={selectableIDs.length === 0}
                                className="inline-flex items-center gap-1.5 hover:text-slate-900 disabled:opacity-50 transition-colors"
                            >
                                <CheckSquare checked={allLoadedSelected} />
                                {allLoadedSelected ? "Clear loaded" : `Select loaded (${selectableIDs.length})`}
                            </button>
                            {!rowSel.all && (search.hasNextPage || contacts.length >= PAGE) && (
                                <button
                                    type="button"
                                    onClick={selectAllMatching}
                                    className="inline-flex items-center gap-1 hover:text-slate-900 transition-colors"
                                >
                                    Select all matching{total != null ? ` (${total.toLocaleString()})` : ""}
                                </button>
                            )}
                            <span className="ml-auto tabular-nums">
                                {count.toLocaleString()} selected
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
                                            ? "Try a different search or label."
                                            : "Import a file or add contacts first."}
                                    </p>
                                </div>
                            ) : (
                                <ul className="divide-y divide-slate-100">
                                    {contacts.map((c) => {
                                        const already = inCampaign(c);
                                        const on = !already && isSelected(c.id);
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
                                                                    color: labelInk(cat.color),
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
                                {target.kind === "campaign"
                                    ? "Contacts already in this campaign are skipped."
                                    : "Added contacts stay in the segment whatever its conditions say."}
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
                                disabled={busy || count === 0}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <UsersIcon className="w-3 h-3" />}
                                Add {count > 0 ? count.toLocaleString() : ""} {target.kind === "campaign" ? "lead" : "contact"}{count === 1 ? "" : "s"}
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
