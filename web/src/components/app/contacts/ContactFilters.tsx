// Contact filters — brae-density side sheet.
//
// Replaces the legacy 800px poppins-serif drawer. New panel is 400px,
// edge-to-edge with a sticky topbar and sticky footer; each filter
// group sits between hairline SectionBars so they read as a quiet
// outline of the available knobs rather than a heavy form.
//
// Local state mirrors the parent's `filters` until the user clicks
// Apply — that way fiddling with filters doesn't immediately retrigger
// the search while they're still building the query.

import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type SearchContactsFilter from "@/lib/api/models/app/contacts/SearchContactsFilter";
import type { SearchContactsFilterType, SearchContactsSortBy } from "@/lib/api/models/app/contacts/search-contacts.types";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowDownIcon,
    ArrowUpIcon,
    CheckIcon,
    Loader2Icon,
    PlusIcon,
    RotateCcwIcon,
    SearchIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";

import { NumberInput, SearchInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { SectionBar } from "@/components/layout/Page";
import CategoryPicker from "./CategoryPicker";

interface Props {
    active: boolean;
    setActive: React.Dispatch<React.SetStateAction<boolean>>;
    filters: SearchContacts;
    setFilters: React.Dispatch<React.SetStateAction<SearchContacts>>;
    activeCampaign?: MiniCampaign;
    loading?: boolean;
}

const SORT_OPTIONS: { id: SearchContactsSortBy; label: string }[] = [
    { id: "created_at", label: "Date added" },
    { id: "updated_at", label: "Last updated" },
    { id: "first_name", label: "First name" },
    { id: "last_name", label: "Last name" },
    { id: "email", label: "Email" },
    { id: "campaign_count", label: "Campaigns count" },
];

const FILTER_TYPES: { id: SearchContactsFilterType; label: string }[] = [
    { id: "contains", label: "Contains" },
    { id: "equal", label: "Equals" },
    { id: "starts_with", label: "Starts with" },
    { id: "ends_with", label: "Ends with" },
];

export default function ContactFilters({
    active,
    setActive,
    filters,
    setFilters,
    activeCampaign,
    loading,
}: Props) {
    const [draft, setDraft] = React.useState<SearchContacts>(filters);

    // When the parent passes in new committed filters, mirror them into
    // the draft so re-opening the sheet doesn't show a stale state.
    React.useEffect(() => {
        if (active) setDraft(filters);
    }, [active, filters]);

    const activeCount = countActiveFilters(draft, !!activeCampaign);

    const apply = () => {
        setFilters(draft);
        setActive(false);
    };
    const reset = () => {
        const empty: SearchContacts = {
            query: "",
            filters: [],
            campaign_ids: activeCampaign ? [activeCampaign.id] : [],
            sort_by: "created_at",
            reverse: false,
        };
        setDraft(empty);
    };

    return (
        <AnimatePresence>
            {active && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.18 }}
                    onClick={() => setActive(false)}
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
                        {/* Sticky header */}
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
                                onClick={() => setActive(false)}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        {/* Scrollable body */}
                        <div className="flex-1 min-h-0 overflow-y-auto">
                            <Section label="Search" />
                            <div className="px-4 py-3">
                                <SearchInput
                                    value={draft.query}
                                    onChange={(v) => setDraft((s) => ({ ...s, query: v }))}
                                    placeholder="Name, email, company…"
                                />
                            </div>

                            <Section
                                label="Custom field filters"
                                count={draft.filters.length}
                                actions={
                                    draft.filters.length < 100 ? (
                                        <button
                                            type="button"
                                            onClick={() =>
                                                setDraft((s) => ({
                                                    ...s,
                                                    filters: [
                                                        ...s.filters,
                                                        { name: "", value: "", type: "contains" },
                                                    ],
                                                }))
                                            }
                                            className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                                        >
                                            <PlusIcon className="w-3 h-3" />
                                            Add
                                        </button>
                                    ) : null
                                }
                            />
                            <div className="px-4 py-2 space-y-2">
                                {draft.filters.length === 0 ? (
                                    <p className="text-[11.5px] text-slate-400 py-2">
                                        Add a filter to query custom contact properties.
                                    </p>
                                ) : (
                                    draft.filters.map((f, i) => (
                                        <FilterRow
                                            key={i}
                                            value={f}
                                            onChange={(updated) =>
                                                setDraft((s) => ({
                                                    ...s,
                                                    filters: s.filters.map((it, idx) =>
                                                        idx === i ? updated : it,
                                                    ),
                                                }))
                                            }
                                            onRemove={() =>
                                                setDraft((s) => ({
                                                    ...s,
                                                    filters: s.filters.filter((_, idx) => idx !== i),
                                                }))
                                            }
                                        />
                                    ))
                                )}
                            </div>

                            <Section label="Sort" />
                            <div className="px-4 py-3 flex items-center gap-1.5">
                                <PopoverMenu align="start">
                                    <PopoverMenuTrigger asChild>
                                        <SelectButton
                                            label={
                                                SORT_OPTIONS.find((o) => o.id === draft.sort_by)?.label ?? "Date added"
                                            }
                                            className="flex-1"
                                        />
                                    </PopoverMenuTrigger>
                                    <PopoverMenuContent minWidth={220}>
                                        <PopoverMenuLabel>Sort by</PopoverMenuLabel>
                                        {SORT_OPTIONS.map((o) => (
                                            <PopoverMenuItem
                                                key={o.id}
                                                selected={draft.sort_by === o.id}
                                                onSelect={() =>
                                                    setDraft((s) => ({ ...s, sort_by: o.id }))
                                                }
                                            >
                                                {o.label}
                                            </PopoverMenuItem>
                                        ))}
                                    </PopoverMenuContent>
                                </PopoverMenu>
                                <button
                                    type="button"
                                    onClick={() => setDraft((s) => ({ ...s, reverse: !s.reverse }))}
                                    className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors"
                                    title={draft.reverse ? "Ascending" : "Descending"}
                                >
                                    {draft.reverse ? (
                                        <ArrowUpIcon className="w-3 h-3" />
                                    ) : (
                                        <ArrowDownIcon className="w-3 h-3" />
                                    )}
                                    {draft.reverse ? "Asc" : "Desc"}
                                </button>
                            </div>

                            <Section
                                label="Categories"
                                count={draft.category_ids?.length ?? 0}
                            />
                            <div className="px-4 py-3">
                                <CategoryPicker
                                    value={draft.category_ids ?? []}
                                    onChange={(next) =>
                                        setDraft((s) => ({
                                            ...s,
                                            category_ids: next.length > 0 ? next : undefined,
                                        }))
                                    }
                                    placeholder="Filter by categories…"
                                />
                                <p className="text-[10.5px] text-slate-400 mt-1.5 leading-tight">
                                    Contacts must have every selected category.
                                </p>
                            </div>

                            <Section label="Subscription" />
                            <div className="px-4 py-3">
                                <Toggle3
                                    value={draft.subscribed}
                                    onChange={(v) => setDraft((s) => ({ ...s, subscribed: v }))}
                                    options={[
                                        { id: undefined, label: "Any" },
                                        { id: true, label: "Subscribed" },
                                        { id: false, label: "Unsubscribed" },
                                    ]}
                                />
                            </div>

                            <Section label="Campaign membership" />
                            <div className="px-4 py-3 space-y-2">
                                <RangeRow
                                    label="At least"
                                    value={draft.min_campaigns}
                                    onChange={(v) => setDraft((s) => ({ ...s, min_campaigns: v }))}
                                    suffix="campaigns"
                                />
                                <RangeRow
                                    label="At most"
                                    value={draft.max_campaigns}
                                    onChange={(v) => setDraft((s) => ({ ...s, max_campaigns: v }))}
                                    suffix="campaigns"
                                />
                            </div>

                            <Section label="Dates" />
                            <div className="px-4 py-3 space-y-2">
                                <DateRow
                                    label="Created after"
                                    value={draft.created_after}
                                    onChange={(v) => setDraft((s) => ({ ...s, created_after: v }))}
                                />
                                <DateRow
                                    label="Created before"
                                    value={draft.created_before}
                                    onChange={(v) => setDraft((s) => ({ ...s, created_before: v }))}
                                />
                                <DateRow
                                    label="Updated after"
                                    value={draft.updated_after}
                                    onChange={(v) => setDraft((s) => ({ ...s, updated_after: v }))}
                                />
                                <DateRow
                                    label="Updated before"
                                    value={draft.updated_before}
                                    onChange={(v) => setDraft((s) => ({ ...s, updated_before: v }))}
                                />
                            </div>
                        </div>

                        {/* Sticky footer */}
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
                                onClick={() => setActive(false)}
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

function Section({ label, count, actions }: { label: string; count?: number; actions?: React.ReactNode }) {
    return (
        <SectionBar label={label} count={count}>
            {actions}
        </SectionBar>
    );
}

function FilterRow({
    value,
    onChange,
    onRemove,
}: {
    value: SearchContactsFilter;
    onChange: (v: SearchContactsFilter) => void;
    onRemove: () => void;
}) {
    return (
        <div className="flex flex-wrap items-center gap-1.5 sm:flex-nowrap">
            <TextInput
                value={value.name}
                onChange={(v) => onChange({ ...value, name: v })}
                placeholder="field"
                className="min-w-[140px] flex-1 sm:min-w-0"
            />
            <PopoverMenu align="start">
                <PopoverMenuTrigger asChild>
                    <SelectButton
                        label={FILTER_TYPES.find((t) => t.id === value.type)?.label ?? "Contains"}
                    />
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={140}>
                    {FILTER_TYPES.map((t) => (
                        <PopoverMenuItem
                            key={t.id}
                            selected={value.type === t.id}
                            onSelect={() => onChange({ ...value, type: t.id })}
                        >
                            {t.label}
                        </PopoverMenuItem>
                    ))}
                </PopoverMenuContent>
            </PopoverMenu>
            <TextInput
                value={value.value}
                onChange={(v) => onChange({ ...value, value: v })}
                placeholder="value"
                className="min-w-[140px] flex-1 sm:min-w-0"
            />
            <button
                type="button"
                onClick={onRemove}
                aria-label="Remove filter"
                className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors shrink-0"
            >
                <Trash2Icon className="w-3 h-3" />
            </button>
        </div>
    );
}

function Toggle3<T extends boolean | undefined>({
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
                        value === o.id
                            ? "bg-slate-900 text-white"
                            : "text-slate-500 hover:text-slate-900"
                    }`}
                >
                    {o.label}
                </button>
            ))}
        </div>
    );
}

function RangeRow({
    label,
    value,
    onChange,
    suffix,
}: {
    label: string;
    value: number | undefined;
    onChange: (v: number | undefined) => void;
    suffix: string;
}) {
    const enabled = value !== undefined;
    return (
        <div className="flex items-center gap-2">
            <button
                type="button"
                onClick={() => onChange(enabled ? undefined : 0)}
                className={`size-4 rounded border flex items-center justify-center transition-colors shrink-0 ${
                    enabled
                        ? "bg-slate-900 border-slate-900 text-white"
                        : "border-slate-300 hover:border-slate-400"
                }`}
                aria-pressed={enabled}
                aria-label={`Toggle ${label}`}
            >
                {enabled && <CheckIcon className="w-2.5 h-2.5" />}
            </button>
            <span className="text-[12px] text-slate-700 w-20 shrink-0">{label}</span>
            <NumberInput
                value={value ?? Number.NaN}
                onChange={(n) => onChange(n)}
                min={0}
                align="right"
                placeholder="0"
                disabled={!enabled}
                className="w-20"
            />
            <span className="text-[11.5px] text-slate-400">{suffix}</span>
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
                {enabled && <CheckIcon className="w-2.5 h-2.5" />}
            </button>
            <span className="text-[12px] text-slate-700 w-28 shrink-0">{label}</span>
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

function countActiveFilters(f: SearchContacts, hasCampaignContext: boolean): number {
    let n = 0;
    if (f.query) n++;
    n += f.filters.length;
    if (f.subscribed !== undefined) n++;
    if (f.min_campaigns !== undefined) n++;
    if (f.max_campaigns !== undefined) n++;
    if (f.created_after) n++;
    if (f.created_before) n++;
    if (f.updated_after) n++;
    if (f.updated_before) n++;
    // Don't count campaign scoping if it's coming from an outer page context.
    if (!hasCampaignContext && f.campaign_ids.length > 0) n++;
    if (f.category_ids && f.category_ids.length > 0) n++;
    return n;
}
