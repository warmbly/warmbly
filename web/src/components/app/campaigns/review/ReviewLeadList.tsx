import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertTriangleIcon, CheckIcon, ListFilterIcon, PencilLineIcon, XIcon } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { SearchInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger, SelectButton } from "@/components/ui/popover-menu";
import type { CampaignReviewSummary, MissingField, ReviewLead, ReviewStatus } from "@/lib/api/models/app/campaigns/Review";
import { MISSING_FIELD_LABEL, leadName } from "@/lib/api/models/app/campaigns/Review";
import { cn } from "@/lib/utils";
import { STATUS_PILL, filterCount } from "./reviewStatus";
import { LeadRowsSkeleton, RunningGrid } from "./reviewLook";

type Filter = "all" | ReviewStatus;

const FILTERS: { key: Filter; label: string; hint: string }[] = [
    { key: "waiting", label: "Waiting", hint: "Waiting for someone to approve their first email" },
    { key: "ready", label: "Ready", hint: "Not emailed yet; their first email goes out on schedule" },
    { key: "started", label: "Started", hint: "Already received their first email" },
    { key: "held", label: "Paused", hint: "Not emailed yet, and paused for another reason" },
    { key: "all", label: "All leads", hint: "Every lead in the campaign" },
];

export default function ReviewLeadList({
    leads,
    loading,
    failed,
    onRetry,
    filtered,
    hasMore,
    loadingMore,
    onLoadMore,
    activeId,
    onOpen,
    selected,
    onToggle,
    onToggleAll,
    search,
    onSearch,
    status,
    onStatus,
    counts,
    reviewMode,
    missing,
    onClearMissing,
    emptyText,
    writing,
}: {
    leads: ReviewLead[];
    loading: boolean;
    failed: boolean;
    onRetry: () => void;
    // A search or missing-field filter, which the campaign counts do not cover.
    filtered: boolean;
    hasMore: boolean;
    loadingMore: boolean;
    onLoadMore: () => void;
    activeId: string | null;
    onOpen: (id: string) => void;
    selected: Set<string>;
    onToggle: (id: string) => void;
    onToggleAll: (all: boolean) => void;
    search: string;
    onSearch: (v: string) => void;
    status: Filter;
    onStatus: (s: Filter) => void;
    counts: CampaignReviewSummary["counts"];
    reviewMode: boolean;
    missing?: MissingField;
    onClearMissing: () => void;
    emptyText: string;
    writing: Set<string>;
}) {
    const sentinel = React.useRef<HTMLDivElement | null>(null);
    React.useEffect(() => {
        const el = sentinel.current;
        if (!el || !hasMore) return;
        const io = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting) && !loadingMore) onLoadMore();
        });
        io.observe(el);
        return () => io.disconnect();
    }, [hasMore, loadingMore, onLoadMore]);

    // Keep the open lead in view as J and K move through the list, and once the list loads.
    const listRef = React.useRef<HTMLUListElement | null>(null);
    React.useEffect(() => {
        listRef.current?.querySelector<HTMLElement>("[data-active='true']")?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }, [activeId, loading]);

    const allSelected = leads.length > 0 && leads.every((l) => selected.has(l.contact_id));
    // Waiting only means something with approval on; Paused only when someone is.
    const filters = FILTERS.filter((f) => (f.key === "waiting" ? reviewMode || counts.waiting > 0 : f.key === "held" ? counts.held > 0 : true));
    const current = FILTERS.find((f) => f.key === status) ?? FILTERS[4];
    const total = filterCount(counts, status);
    const at = leads.findIndex((l) => l.contact_id === activeId);
    // Filtered results have no server total, so it is known only once every page is loaded.
    const shown = filtered ? (hasMore ? undefined : leads.length) : total;

    return (
        <div className="flex flex-col min-h-0 h-full">
            <div className="shrink-0 px-3 py-2 flex items-center gap-2 border-b border-slate-200">
                <Checkbox aria-label="Select all shown" checked={allSelected} onChange={(e) => onToggleAll(e.target.checked)} disabled={leads.length === 0} />
                <SearchInput value={search} onChange={onSearch} placeholder="Search leads" className="flex-1 min-w-0" />
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<ListFilterIcon className="w-3.5 h-3.5" />}
                            label={`${current.label} ${total.toLocaleString()}`}
                            aria-label={`Showing: ${current.label}`}
                            title="Which leads to show"
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={220} className="p-1">
                        {filters.map((f) => (
                            <PopoverMenuItem
                                key={f.key}
                                selected={f.key === status}
                                onSelect={() => onStatus(f.key)}
                                description={f.hint}
                                trailing={
                                    <span className="inline-flex items-center gap-1.5 tabular-nums text-[11px] text-slate-400">
                                        {filterCount(counts, f.key).toLocaleString()}
                                        <CheckIcon className={cn("w-3 h-3 text-sky-600", f.key !== status && "invisible")} />
                                    </span>
                                }
                            >
                                {f.label}
                            </PopoverMenuItem>
                        ))}
                    </PopoverMenuContent>
                </PopoverMenu>
            </div>
            <AnimatePresence initial={false}>
                {missing && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="shrink-0 overflow-hidden border-b border-slate-200 bg-amber-50/70"
                    >
                        <div className="h-8 px-3 flex items-center gap-2 text-[12px] text-amber-800">
                            <AlertTriangleIcon className="w-3.5 h-3.5" />
                            <span className="flex-1 min-w-0 truncate">No {MISSING_FIELD_LABEL[missing] ?? missing}</span>
                            <button type="button" onClick={onClearMissing} aria-label="Clear filter" className="size-6 rounded-md hover:bg-amber-100 flex items-center justify-center">
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
            <div className="flex-1 min-h-0 overflow-y-auto">
                {loading ? (
                    <LeadRowsSkeleton />
                ) : failed && leads.length === 0 ? (
                    <div className="p-8 flex flex-col items-center gap-2 text-[12.5px] text-slate-500">
                        Could not load the leads.
                        <button type="button" onClick={onRetry} className="text-sky-700 hover:text-sky-800 font-medium">
                            Try again
                        </button>
                    </div>
                ) : leads.length === 0 ? (
                    <motion.p initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="p-8 text-center text-[12.5px] text-slate-500">
                        {emptyText}
                    </motion.p>
                ) : (
                    <ul ref={listRef}>
                        <AnimatePresence initial={false}>
                            {leads.map((l) => {
                                const pill = STATUS_PILL[l.status];
                                const active = l.contact_id === activeId;
                                const working = writing.has(l.contact_id);
                                return (
                                    <motion.li
                                        key={l.contact_id}
                                        layout="position"
                                        initial={{ opacity: 0 }}
                                        animate={{ opacity: 1 }}
                                        exit={{ opacity: 0, x: 24, height: 0, transition: { duration: 0.22, ease: "easeIn" } }}
                                        className="overflow-hidden"
                                    >
                                        <div
                                            role="button"
                                            tabIndex={0}
                                            data-active={active}
                                            title={l.email}
                                            onClick={() => onOpen(l.contact_id)}
                                            onKeyDown={(e) => {
                                                if (e.target !== e.currentTarget || (e.key !== "Enter" && e.key !== " ")) return;
                                                e.preventDefault();
                                                onOpen(l.contact_id);
                                            }}
                                            className={cn(
                                                "group relative h-10 pl-3 pr-3 flex items-center gap-2.5 cursor-pointer outline-none transition-colors focus-visible:bg-slate-50",
                                                active ? "bg-sky-50/80" : working ? "bg-sky-50/30 hover:bg-sky-50/60" : "hover:bg-slate-50",
                                            )}
                                        >
                                            {active && <motion.span layoutId="review-active-row" className="absolute left-0 top-1 bottom-1 w-0.5 rounded-full bg-sky-600" />}
                                            {working && (
                                                <span aria-hidden className="absolute inset-x-0 bottom-0 h-px overflow-hidden">
                                                    <span className="absolute inset-y-0 w-1/3 bg-gradient-to-r from-transparent via-sky-400 to-transparent progress-sweep" />
                                                </span>
                                            )}
                                            <span onClick={(e) => e.stopPropagation()} className="flex">
                                                <Checkbox aria-label={`Select ${leadName(l)}`} checked={selected.has(l.contact_id)} onChange={() => onToggle(l.contact_id)} />
                                            </span>
                                            <span className="min-w-0 flex-1 flex items-baseline gap-1.5">
                                                <span className={cn("text-[12.5px] truncate", active ? "font-semibold text-slate-900" : "font-medium text-slate-800")}>{leadName(l)}</span>
                                                {l.company && <span className="text-[11.5px] text-slate-400 truncate">{l.company}</span>}
                                            </span>
                                            {l.personalized > 0 && <PencilLineIcon className="w-3 h-3 text-sky-600 shrink-0" aria-label="Personalized" />}
                                            {(l.missing?.length ?? 0) > 0 && (
                                                <span title={`No ${l.missing!.map((m) => MISSING_FIELD_LABEL[m] ?? m).join(", ")}`} className="flex">
                                                    <AlertTriangleIcon className="w-3 h-3 text-amber-500 shrink-0" />
                                                </span>
                                            )}
                                            {working ? (
                                                <span title="AI is writing this lead's copy" className="flex">
                                                    <RunningGrid className="text-sky-600" />
                                                </span>
                                            ) : (
                                                <span title={pill.label} aria-label={pill.label} className={cn("shrink-0 size-1.5 rounded-full", pill.dot)} />
                                            )}
                                        </div>
                                    </motion.li>
                                );
                            })}
                        </AnimatePresence>
                    </ul>
                )}
                {hasMore && (
                    <div ref={sentinel}>
                        {loadingMore ? (
                            <LeadRowsSkeleton rows={3} />
                        ) : (
                            <div className="p-3 flex justify-center">
                                <button type="button" onClick={onLoadMore} className="text-[12px] text-sky-700 hover:text-sky-800">
                                    Load more
                                </button>
                            </div>
                        )}
                    </div>
                )}
            </div>
            <div className="shrink-0 h-8 px-3 flex items-center gap-2 border-t border-slate-200 text-[11px] text-slate-400">
                <span className="tabular-nums">
                    {shown === undefined
                        ? `${leads.length.toLocaleString()}+ matches`
                        : at >= 0
                          ? `${(at + 1).toLocaleString()} of ${shown.toLocaleString()}`
                          : `${shown.toLocaleString()} ${shown === 1 ? "lead" : "leads"}`}
                </span>
                <span className="ml-auto hidden md:inline-flex items-center gap-1">
                    <Kbd>J</Kbd>
                    <Kbd>K</Kbd> to move
                    {reviewMode && (
                        <>
                            <span className="mx-1">·</span>
                            <Kbd>A</Kbd> to approve
                        </>
                    )}
                </span>
            </div>
        </div>
    );
}

function Kbd({ children }: { children: React.ReactNode }) {
    return <kbd className="min-w-4 h-4 px-1 rounded border border-slate-200 bg-white font-sans text-[10px] text-slate-500 inline-flex items-center justify-center">{children}</kbd>;
}
