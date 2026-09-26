// Interactive filter bar for the contact list: quick pills for categories,
// segments, status and campaigns, an "Add filter" menu for the rest, and every
// change applied on the spot. Pills open a popover under themselves.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, ChevronDownIcon, LayersIcon, Loader2Icon, PlusIcon, XIcon } from "lucide-react";

import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { DatePicker } from "@/components/ui/DatePicker";
import { NumberInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { SelectMenu } from "@/components/ui/select-menu";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import { useSegments } from "@/lib/api/hooks/app/segments";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type SearchContactsFilter from "@/lib/api/models/app/contacts/SearchContactsFilter";
import type { SearchContactsFilterType } from "@/lib/api/models/app/contacts/search-contacts.types";
import type { LeadEngagement, LeadStatus, VerificationStatus } from "@/lib/api/models/app/contacts/Contact";
import type { MailHost } from "@/lib/api/models/app/emails/MailboxImport";
import { MAIL_HOST_LABELS } from "@/lib/mailHost";
import { cn } from "@/lib/utils";
import { countActiveFilters, isCompleteCustomFilter } from "./helpers";

type Setter = React.Dispatch<React.SetStateAction<SearchContacts>>;

const FILTER_TYPES: { id: SearchContactsFilterType; label: string }[] = [
    { id: "contains", label: "contains" },
    { id: "equal", label: "is" },
    { id: "starts_with", label: "starts with" },
    { id: "ends_with", label: "ends with" },
];

const LEAD_STATUS: { id: LeadStatus; label: string }[] = [
    { id: "pending", label: "Queued" },
    { id: "active", label: "Processing" },
    { id: "completed", label: "Done" },
    { id: "replied", label: "Replied" },
    { id: "bounced", label: "Bounced" },
    { id: "failed", label: "Failed" },
    { id: "paused", label: "Paused" },
    { id: "undeliverable", label: "Undeliverable" },
];

const ENGAGEMENT: { id: LeadEngagement; label: string }[] = [
    { id: "opened", label: "Opened" },
    { id: "not_opened", label: "Not opened" },
    { id: "clicked", label: "Clicked" },
    { id: "not_clicked", label: "Not clicked" },
    { id: "replied", label: "Replied" },
    { id: "not_replied", label: "Not replied" },
];

const VERIFICATION: { id: VerificationStatus; label: string }[] = [
    { id: "valid", label: "Deliverable" },
    { id: "risky", label: "Risky" },
    { id: "invalid", label: "Undeliverable" },
    { id: "unknown", label: "Unverified" },
];

// Every inbox host the backend detects, then the contacts it has not reached yet.
const PROVIDERS: Option[] = [
    ...(Object.entries(MAIL_HOST_LABELS) as [MailHost, string][]).map(([id, label]) => ({
        id,
        label,
        icon: <ProviderLogo id={id} size="xs" framed={false} />,
    })),
    { id: "", label: "Not detected yet" },
];

// Optional pills, shown once added from the menu or when their value is set.
type ExtraKey = "created" | "updated" | "campaign_count" | "lead_status" | "engagement" | "verification" | "provider";

function toIso(d?: Date): string {
    return d ? new Date(d).toISOString().slice(0, 10) : "";
}
function fromIso(s: string): Date | undefined {
    return s ? new Date(`${s}T00:00:00`) : undefined;
}

export default function FilterBar({
    filters,
    setFilters,
    activeCampaign,
    hideSegments,
    resetToken,
    total,
    loading,
    onSaveAsSegment,
}: {
    filters: SearchContacts;
    setFilters: Setter;
    activeCampaign?: MiniCampaign;
    // On a segment page the segment scope is fixed, so the pill is hidden.
    hideSegments?: boolean;
    // Bumped when the parent clears the filters, so blank extra pills go too.
    resetToken?: number;
    total: number;
    loading?: boolean;
    onSaveAsSegment?: (draft: SearchContacts) => void;
}) {
    const { user } = useUserProfile();
    const segments = useSegments();
    const customKeys = useCustomFieldKeys();
    const campaignCtx = !!activeCampaign;

    // Pills added from the menu stay visible while empty so the user can fill them.
    const [extras, setExtras] = React.useState<ExtraKey[]>([]);
    const [openKey, setOpenKey] = React.useState<string | null>(null);

    const shown = (k: ExtraKey) => {
        if (extras.includes(k)) return true;
        switch (k) {
            case "created":
                return !!(filters.created_after || filters.created_before);
            case "updated":
                return !!(filters.updated_after || filters.updated_before);
            case "campaign_count":
                return filters.min_campaigns !== undefined || filters.max_campaigns !== undefined;
            case "lead_status":
                return !!filters.lead_status;
            case "engagement":
                return !!filters.engagement;
            case "verification":
                return !!filters.verification_status;
            case "provider":
                return !!filters.mail_hosts?.length;
        }
    };

    function addExtra(k: ExtraKey) {
        setExtras((e) => (e.includes(k) ? e : [...e, k]));
        setOpenKey(k);
    }
    function removeExtra(k: ExtraKey) {
        setExtras((e) => e.filter((x) => x !== k));
        setFilters((s) => {
            switch (k) {
                case "created":
                    return { ...s, created_after: undefined, created_before: undefined };
                case "updated":
                    return { ...s, updated_after: undefined, updated_before: undefined };
                case "campaign_count":
                    return { ...s, min_campaigns: undefined, max_campaigns: undefined };
                case "lead_status":
                    return { ...s, lead_status: undefined };
                case "engagement":
                    return { ...s, engagement: undefined };
                case "verification":
                    return { ...s, verification_status: undefined };
                case "provider":
                    return { ...s, mail_hosts: undefined };
            }
        });
    }

    function addCustom() {
        setFilters((s) => ({ ...s, custom_field_filters: [...s.custom_field_filters, { name: "", value: "", type: "contains" }] }));
        setOpenKey(`custom:${filters.custom_field_filters.length}`);
    }
    function setCustom(i: number, next: SearchContactsFilter) {
        setFilters((s) => ({ ...s, custom_field_filters: s.custom_field_filters.map((f, j) => (j === i ? next : f)) }));
    }
    function removeCustom(i: number) {
        setFilters((s) => ({ ...s, custom_field_filters: s.custom_field_filters.filter((_, j) => j !== i) }));
        setOpenKey(null);
    }

    React.useEffect(() => {
        if (resetToken === undefined) return;
        setExtras([]);
        setOpenKey(null);
    }, [resetToken]);

    const active = countActiveFilters(filters, campaignCtx);
    function clearAll() {
        setExtras([]);
        setOpenKey(null);
        setFilters((s) => ({
            query: s.query,
            custom_field_filters: [],
            campaign_ids: activeCampaign ? [activeCampaign.id] : [],
            segment_ids: hideSegments ? s.segment_ids : undefined,
            sort_by: s.sort_by,
            reverse: s.reverse,
        }));
    }

    const categoryOptions = React.useMemo(
        () => [...(user.categories ?? [])].sort((a, b) => a.position - b.position).map((c) => ({ id: c.id, label: c.title, color: c.color })),
        [user.categories],
    );
    const segmentOptions = React.useMemo(
        () => (segments.data ?? []).map((s) => ({ id: s.id, label: s.name, color: s.color })),
        [segments.data],
    );

    const menuItems: { key: ExtraKey | "custom"; label: string; hidden?: boolean }[] = [
        { key: "custom", label: "Custom field" },
        { key: "created", label: "Date added", hidden: shown("created") },
        { key: "updated", label: "Last updated", hidden: shown("updated") },
        { key: "campaign_count", label: "Number of campaigns", hidden: shown("campaign_count") },
        { key: "verification", label: "Address verification", hidden: shown("verification") },
        { key: "provider", label: "Email provider", hidden: shown("provider") },
        { key: "lead_status", label: "Lead status", hidden: !campaignCtx || shown("lead_status") },
        { key: "engagement", label: "Engagement", hidden: !campaignCtx || shown("engagement") },
    ];

    return (
        <div className="px-5 py-1.5 border-b border-slate-200/60 bg-white flex flex-wrap items-center gap-1.5">
            <MultiPill
                id="categories"
                label="Category"
                openKey={openKey}
                setOpenKey={setOpenKey}
                value={filters.category_ids ?? []}
                onChange={(v) => setFilters((s) => ({ ...s, category_ids: v.length ? v : undefined }))}
                options={categoryOptions}
                empty="No categories yet."
                hint="Contacts must have every selected category."
            />
            {!hideSegments && (
                <MultiPill
                    id="segments"
                    label="Segment"
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    value={filters.segment_ids ?? []}
                    onChange={(v) => setFilters((s) => ({ ...s, segment_ids: v.length ? v : undefined }))}
                    options={segmentOptions}
                    empty="No segments yet."
                    hint="Contacts must be in every selected segment."
                />
            )}
            <ChoicePill<boolean | undefined>
                id="status"
                label="Status"
                openKey={openKey}
                setOpenKey={setOpenKey}
                value={filters.subscribed}
                onChange={(v) => setFilters((s) => ({ ...s, subscribed: v }))}
                options={[
                    { id: true, label: "Subscribed" },
                    { id: false, label: "Unsubscribed" },
                ]}
            />
            {!campaignCtx && (
                <CampaignPill
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    value={filters.campaign_ids}
                    onChange={(v) => setFilters((s) => ({ ...s, campaign_ids: v }))}
                />
            )}

            {filters.custom_field_filters.map((f, i) => (
                <CustomPill
                    key={`custom:${i}`}
                    id={`custom:${i}`}
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    value={f}
                    keys={customKeys.data ?? []}
                    onChange={(next) => setCustom(i, next)}
                    onRemove={() => removeCustom(i)}
                />
            ))}
            {shown("created") && (
                <DateRangePill
                    id="created"
                    label="Added"
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    after={filters.created_after}
                    before={filters.created_before}
                    onChange={(after, before) => setFilters((s) => ({ ...s, created_after: after, created_before: before }))}
                    onRemove={() => removeExtra("created")}
                />
            )}
            {shown("updated") && (
                <DateRangePill
                    id="updated"
                    label="Updated"
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    after={filters.updated_after}
                    before={filters.updated_before}
                    onChange={(after, before) => setFilters((s) => ({ ...s, updated_after: after, updated_before: before }))}
                    onRemove={() => removeExtra("updated")}
                />
            )}
            {shown("campaign_count") && (
                <RangePill
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    min={filters.min_campaigns}
                    max={filters.max_campaigns}
                    onChange={(min, max) => setFilters((s) => ({ ...s, min_campaigns: min, max_campaigns: max }))}
                    onRemove={() => removeExtra("campaign_count")}
                />
            )}
            {shown("verification") && (
                <ChoicePill<VerificationStatus | undefined>
                    id="verification"
                    label="Verification"
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    value={filters.verification_status}
                    onChange={(v) => setFilters((s) => ({ ...s, verification_status: v }))}
                    options={VERIFICATION}
                    onRemove={() => removeExtra("verification")}
                />
            )}
            {shown("provider") && (
                <Pill
                    id="provider"
                    label="Provider"
                    summary={summarize(filters.mail_hosts ?? [], PROVIDERS)}
                    active={!!filters.mail_hosts?.length}
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    onRemove={() => removeExtra("provider")}
                >
                    <CheckList
                        value={filters.mail_hosts ?? []}
                        onChange={(v) => setFilters((s) => ({ ...s, mail_hosts: v.length ? (v as MailHost[]) : undefined }))}
                        options={PROVIDERS}
                        empty="No providers."
                        hint="Contacts at any selected provider."
                    />
                </Pill>
            )}
            {shown("lead_status") && (
                <ChoicePill<LeadStatus | undefined>
                    id="lead_status"
                    label="Lead status"
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    value={filters.lead_status}
                    onChange={(v) => setFilters((s) => ({ ...s, lead_status: v }))}
                    options={LEAD_STATUS}
                    onRemove={() => removeExtra("lead_status")}
                />
            )}
            {shown("engagement") && (
                <ChoicePill<LeadEngagement | undefined>
                    id="engagement"
                    label="Engagement"
                    openKey={openKey}
                    setOpenKey={setOpenKey}
                    value={filters.engagement}
                    onChange={(v) => setFilters((s) => ({ ...s, engagement: v }))}
                    options={ENGAGEMENT}
                    onRemove={() => removeExtra("engagement")}
                />
            )}

            <PopoverMenu align="start">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        className="h-7 px-2 rounded-md border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-800 text-[12px] inline-flex items-center gap-1 transition-colors"
                    >
                        <PlusIcon className="w-3 h-3" />
                        Add filter
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={190}>
                    <PopoverMenuLabel>Filter by</PopoverMenuLabel>
                    {menuItems
                        .filter((m) => !m.hidden)
                        .map((m) => (
                            <PopoverMenuItem key={m.key} onSelect={() => (m.key === "custom" ? addCustom() : addExtra(m.key))}>
                                {m.label}
                            </PopoverMenuItem>
                        ))}
                </PopoverMenuContent>
            </PopoverMenu>

            <div className="ml-auto flex items-center gap-1.5">
                <span className="text-[11.5px] text-slate-500 tabular-nums inline-flex items-center gap-1.5">
                    {loading && <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />}
                    {total.toLocaleString()} {activeCampaign ? (total === 1 ? "lead" : "leads") : total === 1 ? "contact" : "contacts"}
                </span>
                {active > 0 && (
                    <button
                        type="button"
                        onClick={clearAll}
                        className="h-7 px-2 rounded-md text-[12px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        Clear
                    </button>
                )}
                {onSaveAsSegment && active > 0 && (
                    <button
                        type="button"
                        onClick={() => onSaveAsSegment(filters)}
                        className="h-7 px-2 rounded-md text-[12px] text-sky-700 hover:text-sky-800 hover:bg-sky-50 inline-flex items-center gap-1 transition-colors"
                    >
                        <LayersIcon className="w-3 h-3" />
                        Save as segment
                    </button>
                )}
            </div>
        </div>
    );
}

// Shared pill shell: trigger chip plus a floating panel beneath it. Only one
// pill is open at a time (openKey lives in the bar).
function Pill({
    id,
    label,
    summary,
    active,
    openKey,
    setOpenKey,
    onRemove,
    width = 260,
    children,
}: {
    id: string;
    label: string;
    summary?: string;
    active: boolean;
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    onRemove?: () => void;
    width?: number;
    children: React.ReactNode;
}) {
    const open = openKey === id;
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(ref, () => {
        if (open) setOpenKey(null);
    });
    const placement = useFlipPlacement(triggerRef, open, 300);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") setOpenKey(null);
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, setOpenKey]);

    return (
        <div ref={ref} className="relative">
            <div
                className={cn(
                    "h-7 rounded-md border text-[12px] inline-flex items-center transition-colors",
                    active ? "border-sky-200 bg-sky-50 text-sky-800" : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
                    open && "ring-2 ring-sky-100",
                )}
            >
                <button
                    ref={triggerRef}
                    type="button"
                    onClick={() => setOpenKey(open ? null : id)}
                    aria-expanded={open}
                    className="h-full pl-2 pr-1.5 inline-flex items-center gap-1 max-w-[280px]"
                >
                    <span className={cn(active ? "text-sky-600" : "text-slate-500")}>{label}</span>
                    {active && summary && (
                        <>
                            <span className="text-sky-400">:</span>
                            <span className="font-medium truncate">{summary}</span>
                        </>
                    )}
                    <ChevronDownIcon className={cn("w-3 h-3 shrink-0 transition-transform", open && "rotate-180", active ? "text-sky-500" : "text-slate-400")} />
                </button>
                {onRemove && (
                    <button
                        type="button"
                        onClick={(e) => {
                            e.stopPropagation();
                            onRemove();
                        }}
                        aria-label={`Remove ${label} filter`}
                        className="h-full pr-1.5 pl-0.5 inline-flex items-center text-slate-400 hover:text-slate-900"
                    >
                        <XIcon className="w-3 h-3" />
                    </button>
                )}
            </div>
            <AnimatePresence>
                {open && (
                    <motion.div
                        data-floating
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        style={{ width }}
                        className={cn(
                            "absolute left-0 z-40 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden max-w-[calc(100vw-2.5rem)]",
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                        )}
                    >
                        {children}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

interface Option {
    id: string;
    label: string;
    color?: string;
    icon?: React.ReactNode;
}

function summarize(ids: string[], options: Option[]): string {
    if (ids.length === 0) return "";
    const first = options.find((o) => o.id === ids[0])?.label ?? "1";
    return ids.length === 1 ? first : `${first} +${ids.length - 1}`;
}

function CheckList({
    value,
    onChange,
    options,
    empty,
    hint,
    searchable = true,
}: {
    value: string[];
    onChange: (next: string[]) => void;
    options: Option[];
    empty: string;
    hint?: string;
    searchable?: boolean;
}) {
    const [query, setQuery] = React.useState("");
    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        return q ? options.filter((o) => o.label.toLowerCase().includes(q)) : options;
    }, [options, query]);
    const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
    return (
        <>
            {searchable && options.length > 6 && (
                <div className="px-2 py-1.5 border-b border-slate-200">
                    <input
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="Search…"
                        autoFocus
                        className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                    />
                </div>
            )}
            <div className="max-h-60 overflow-y-auto py-1">
                {filtered.length === 0 && <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">{options.length === 0 ? empty : "Nothing matches."}</div>}
                {filtered.map((o) => {
                    const checked = value.includes(o.id);
                    return (
                        <button
                            key={o.id}
                            type="button"
                            onClick={() => toggle(o.id)}
                            className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                        >
                            <span
                                className={cn(
                                    "size-3.5 rounded border flex items-center justify-center transition-colors shrink-0",
                                    checked ? "border-slate-900 bg-slate-900" : "border-slate-300 bg-white",
                                )}
                            >
                                {checked && <CheckIcon className="w-2 h-2 text-white" />}
                            </span>
                            {o.color && <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: o.color }} />}
                            {o.icon}
                            <span className="truncate">{o.label}</span>
                        </button>
                    );
                })}
            </div>
            {(hint || value.length > 0) && (
                <div className="px-2.5 h-8 border-t border-slate-100 flex items-center gap-2">
                    {hint && <span className="text-[10.5px] text-slate-400 truncate">{hint}</span>}
                    {value.length > 0 && (
                        <button type="button" onClick={() => onChange([])} className="ml-auto text-[11px] text-slate-500 hover:text-slate-900 shrink-0">
                            Clear
                        </button>
                    )}
                </div>
            )}
        </>
    );
}

function MultiPill({
    id,
    label,
    openKey,
    setOpenKey,
    value,
    onChange,
    options,
    empty,
    hint,
}: {
    id: string;
    label: string;
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    value: string[];
    onChange: (next: string[]) => void;
    options: Option[];
    empty: string;
    hint?: string;
}) {
    return (
        <Pill id={id} label={label} summary={summarize(value, options)} active={value.length > 0} openKey={openKey} setOpenKey={setOpenKey}>
            <CheckList value={value} onChange={onChange} options={options} empty={empty} hint={hint} />
        </Pill>
    );
}

function CampaignPill({
    openKey,
    setOpenKey,
    value,
    onChange,
}: {
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    value: string[];
    onChange: (next: string[]) => void;
}) {
    const open = openKey === "campaigns";
    // Only fetch once the pill is opened or already has a value.
    const campaigns = useCampaigns({ query: "", folder: "", limit: 100, enabled: open || value.length > 0 });
    const options = React.useMemo<Option[]>(() => campaigns.campaigns.map((c) => ({ id: c.id, label: c.name })), [campaigns.campaigns]);
    const { hasNextPage, isFetchingNextPage, fetchNextPage } = campaigns;
    React.useEffect(() => {
        if (open && hasNextPage && !isFetchingNextPage) void fetchNextPage();
    }, [open, hasNextPage, isFetchingNextPage, fetchNextPage]);
    return (
        <Pill id="campaigns" label="Campaign" summary={summarize(value, options)} active={value.length > 0} openKey={openKey} setOpenKey={setOpenKey}>
            <CheckList value={value} onChange={onChange} options={options} empty="No campaigns yet." hint="Contacts in any selected campaign." />
        </Pill>
    );
}

function ChoicePill<T extends string | boolean | undefined>({
    id,
    label,
    openKey,
    setOpenKey,
    value,
    onChange,
    options,
    onRemove,
}: {
    id: string;
    label: string;
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    value: T;
    onChange: (v: T) => void;
    options: { id: T; label: string }[];
    onRemove?: () => void;
}) {
    const current = options.find((o) => o.id === value);
    return (
        <Pill id={id} label={label} summary={current?.label} active={value !== undefined} openKey={openKey} setOpenKey={setOpenKey} onRemove={onRemove} width={200}>
            <div className="py-1">
                {options.map((o) => {
                    const on = o.id === value;
                    return (
                        <button
                            key={String(o.id)}
                            type="button"
                            onClick={() => {
                                onChange((on ? undefined : o.id) as T);
                                setOpenKey(null);
                            }}
                            className={cn(
                                "w-full px-2.5 h-7 flex items-center gap-2 text-[12px] transition-colors hover:bg-slate-100",
                                on ? "text-slate-900 font-medium" : "text-slate-700",
                            )}
                        >
                            <span className={cn("size-3.5 rounded-full border flex items-center justify-center shrink-0", on ? "border-sky-600 bg-sky-600" : "border-slate-300 bg-white")}>
                                {on && <span className="size-1.5 rounded-full bg-white" />}
                            </span>
                            {o.label}
                        </button>
                    );
                })}
                {value !== undefined && (
                    <button
                        type="button"
                        onClick={() => onChange(undefined as T)}
                        className="w-full px-2.5 h-7 flex items-center text-[12px] text-slate-500 hover:bg-slate-100 border-t border-slate-100 mt-1"
                    >
                        Any
                    </button>
                )}
            </div>
        </Pill>
    );
}

function CustomPill({
    id,
    openKey,
    setOpenKey,
    value,
    keys,
    onChange,
    onRemove,
}: {
    id: string;
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    value: SearchContactsFilter;
    keys: string[];
    onChange: (next: SearchContactsFilter) => void;
    onRemove: () => void;
}) {
    // Free text goes through a short debounce so the list does not refetch per key.
    const [text, setText] = React.useState(value.value);
    React.useEffect(() => setText(value.value), [value.value]);
    React.useEffect(() => {
        if (text === value.value) return;
        const t = setTimeout(() => onChange({ ...value, value: text }), 300);
        return () => clearTimeout(t);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [text]);

    const complete = isCompleteCustomFilter(value);
    const op = FILTER_TYPES.find((t) => t.id === value.type)?.label ?? value.type;
    const keyOptions = React.useMemo(() => {
        const set = new Set(keys);
        if (value.name && !set.has(value.name)) set.add(value.name);
        return [...set].map((k) => ({ value: k, label: k }));
    }, [keys, value.name]);

    return (
        <Pill
            id={id}
            label={value.name.trim() ? value.name : "Custom field"}
            summary={complete ? `${op} ${value.value}` : undefined}
            active={complete}
            openKey={openKey}
            setOpenKey={setOpenKey}
            onRemove={onRemove}
            width={300}
        >
            <div className="p-2.5 space-y-2">
                <div className="space-y-1">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Field</span>
                    {keyOptions.length > 0 ? (
                        <SelectMenu value={value.name} onChange={(v) => onChange({ ...value, name: v })} options={keyOptions} placeholder="Pick a field" fullWidth />
                    ) : (
                        <TextInput value={value.name} onChange={(v) => onChange({ ...value, name: v })} placeholder="Field name" autoFocus />
                    )}
                </div>
                <div className="space-y-1">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Condition</span>
                    <SelectMenu
                        value={value.type}
                        onChange={(v) => onChange({ ...value, type: v as SearchContactsFilterType })}
                        options={FILTER_TYPES.map((t) => ({ value: t.id, label: t.label }))}
                        fullWidth
                    />
                </div>
                <div className="space-y-1">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Value</span>
                    <TextInput value={text} onChange={setText} placeholder="Value" autoFocus={keyOptions.length > 0} />
                </div>
            </div>
        </Pill>
    );
}

function DateRangePill({
    id,
    label,
    openKey,
    setOpenKey,
    after,
    before,
    onChange,
    onRemove,
}: {
    id: string;
    label: string;
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    after?: Date;
    before?: Date;
    onChange: (after?: Date, before?: Date) => void;
    onRemove: () => void;
}) {
    const summary = after && before ? `${toIso(after)} to ${toIso(before)}` : after ? `after ${toIso(after)}` : before ? `before ${toIso(before)}` : undefined;
    return (
        <Pill id={id} label={label} summary={summary} active={!!summary} openKey={openKey} setOpenKey={setOpenKey} onRemove={onRemove} width={280}>
            <div className="p-2.5 space-y-2">
                <div className="space-y-1">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">After</span>
                    <DatePicker value={toIso(after)} onChange={(v) => onChange(fromIso(v), before)} className="w-full" />
                </div>
                <div className="space-y-1">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Before</span>
                    <DatePicker value={toIso(before)} onChange={(v) => onChange(after, fromIso(v))} className="w-full" />
                </div>
            </div>
        </Pill>
    );
}

function RangePill({
    openKey,
    setOpenKey,
    min,
    max,
    onChange,
    onRemove,
}: {
    openKey: string | null;
    setOpenKey: (k: string | null) => void;
    min?: number;
    max?: number;
    onChange: (min?: number, max?: number) => void;
    onRemove: () => void;
}) {
    const summary = min !== undefined && max !== undefined ? `${min} to ${max}` : min !== undefined ? `at least ${min}` : max !== undefined ? `at most ${max}` : undefined;
    return (
        <Pill id="campaign_count" label="Campaigns count" summary={summary} active={!!summary} openKey={openKey} setOpenKey={setOpenKey} onRemove={onRemove} width={240}>
            <div className="p-2.5 space-y-2">
                <Bound label="At least" value={min} onChange={(v) => onChange(v, max)} />
                <Bound label="At most" value={max} onChange={(v) => onChange(min, v)} />
            </div>
        </Pill>
    );
}

function Bound({ label, value, onChange }: { label: string; value?: number; onChange: (v?: number) => void }) {
    const set = value !== undefined;
    return (
        <div className="flex items-center gap-2">
            <button
                type="button"
                onClick={() => onChange(set ? undefined : 1)}
                className={cn(
                    "size-3.5 rounded border flex items-center justify-center transition-colors shrink-0",
                    set ? "border-slate-900 bg-slate-900" : "border-slate-300 bg-white",
                )}
                aria-pressed={set}
                aria-label={label}
            >
                {set && <CheckIcon className="w-2 h-2 text-white" />}
            </button>
            <span className="text-[12px] text-slate-700 w-16">{label}</span>
            <NumberInput value={value ?? 0} onChange={(v) => onChange(Math.max(0, v))} min={0} disabled={!set} suffix="campaigns" className="flex-1" />
        </div>
    );
}
