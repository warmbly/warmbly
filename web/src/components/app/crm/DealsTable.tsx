// DealsTable — the cross-pipeline, server-driven "see everything" surface.
//
// Unlike the kanban board (one pipeline, first 100 rows, client-side totals),
// this view:
//   - spans every pipeline by default (pipeline is a column + a facet)
//   - pages the full result set server-side (offset infinite scroll)
//   - reads every header total from /crm/deals/summary, so the Open / Pipeline
//     value / Won numbers are SUMs over the whole filtered set, never a reduce
//     over the loaded page
//   - filters + sorts on the server (status, pipeline, value range, close-date
//     range, text) instead of an in-memory substring match
//
// Visual language mirrors ContactsTable (sticky hairline header, h-11 rows,
// "N of M loaded" footer) so the CRM finally reuses the contacts toolkit.

import React from "react";
import {
    CalendarIcon,
    CircleDollarSignIcon,
    FilterIcon,
    Loader2Icon,
    PlusIcon,
    ArrowUpDownIcon,
    GitBranchIcon,
    UserIcon,
    XIcon,
} from "lucide-react";
import { SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import useSearchDeals from "@/lib/api/hooks/app/crm/deals/useSearchDeals";
import useDealsSummary from "@/lib/api/hooks/app/crm/deals/useDealsSummary";
import type Deal from "@/lib/api/models/app/crm/Deal";
import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import type SearchDeals from "@/lib/api/models/app/crm/SearchDeals";
import type { DealSortBy } from "@/lib/api/models/app/crm/SearchDeals";
import { EMPTY_DEAL_SEARCH } from "@/lib/api/models/app/crm/SearchDeals";

const STATUS_TABS: { id: "all" | "open" | "won" | "lost"; label: string }[] = [
    { id: "all", label: "All" },
    { id: "open", label: "Open" },
    { id: "won", label: "Won" },
    { id: "lost", label: "Lost" },
];

const STATUS_STYLE: Record<Deal["status"], { label: string; cls: string; dot: string }> = {
    open: { label: "Open", cls: "text-slate-600", dot: "bg-slate-400" },
    won: { label: "Won", cls: "text-emerald-700", dot: "bg-emerald-500" },
    lost: { label: "Lost", cls: "text-red-700", dot: "bg-red-500" },
};

const SORTS: { id: string; label: string; sort_by: DealSortBy; reverse: boolean }[] = [
    { id: "newest", label: "Newest", sort_by: "created_at", reverse: false },
    { id: "oldest", label: "Oldest", sort_by: "created_at", reverse: true },
    { id: "value_desc", label: "Value · high → low", sort_by: "value", reverse: false },
    { id: "value_asc", label: "Value · low → high", sort_by: "value", reverse: true },
    { id: "closing", label: "Closing soonest", sort_by: "expected_close_date", reverse: true },
    { id: "name", label: "Name · A → Z", sort_by: "name", reverse: true },
];

export default function DealsTable({
    pipelines,
    onOpenDeal,
}: {
    pipelines: Pipeline[];
    onOpenDeal: (deal: Deal) => void;
}) {
    const [filters, setFilters] = React.useState<SearchDeals>(EMPTY_DEAL_SEARCH);

    const search = useSearchDeals({ filters, limit: 50 });
    const summary = useDealsSummary(filters);
    const deals = search.deals ?? [];
    const total = search.total;
    const sum = summary.data;

    const pipelineName = React.useMemo(() => {
        const m = new Map<string, string>();
        for (const p of pipelines) m.set(p.id, p.name);
        return m;
    }, [pipelines]);

    const statusTab: "all" | "open" | "won" | "lost" =
        filters.statuses.length === 1 ? filters.statuses[0] : "all";

    function setStatusTab(tab: "all" | "open" | "won" | "lost") {
        setFilters((f) => ({ ...f, statuses: tab === "all" ? [] : [tab] }));
    }

    const activeSort =
        SORTS.find((s) => s.sort_by === filters.sort_by && s.reverse === filters.reverse) ?? SORTS[0];

    const advancedCount =
        filters.pipeline_ids.length +
        (filters.min_value != null ? 1 : 0) +
        (filters.max_value != null ? 1 : 0) +
        (filters.close_after ? 1 : 0) +
        (filters.close_before ? 1 : 0);

    return (
        <>
            <StatStrip cols={4}>
                <Stat
                    label="Open"
                    value={sum ? sum.open_count : "—"}
                    sub="open deals"
                />
                <Stat
                    label="Pipeline value"
                    value={sum ? money(sum.open_value, sum.currency) : "—"}
                    sub={sum?.mixed_currency ? "mixed currencies" : "open · server total"}
                />
                <Stat
                    label="Won"
                    value={sum ? money(sum.won_value, sum.currency) : "—"}
                    sub={`${sum?.won_count ?? 0} closed won`}
                />
                <Stat label="Total" value={sum ? sum.total : total} sub="matching filter" last />
            </StatStrip>

            <SectionBar label={search.isPending ? "Loading…" : `${total} ${total === 1 ? "deal" : "deals"}`}>
                <SearchInput
                    value={filters.query}
                    onChange={(v) => setFilters((f) => ({ ...f, query: v }))}
                    placeholder="Search deals…"
                    className="w-full sm:w-[180px]"
                />
                <div className="inline-flex rounded-md bg-slate-100 p-0.5 gap-0.5">
                    {STATUS_TABS.map((t) => (
                        <button
                            key={t.id}
                            type="button"
                            onClick={() => setStatusTab(t.id)}
                            className={`h-6 px-2 rounded text-[11px] font-medium transition-colors ${
                                statusTab === t.id
                                    ? "bg-white text-slate-900 shadow-sm"
                                    : "text-slate-500 hover:text-slate-900"
                            }`}
                        >
                            {t.label}
                        </button>
                    ))}
                </div>
                <PipelineFacet
                    pipelines={pipelines}
                    selected={filters.pipeline_ids}
                    onChange={(ids) => setFilters((f) => ({ ...f, pipeline_ids: ids }))}
                />
                <FilterPopover filters={filters} onChange={setFilters} activeCount={advancedCount} />
                <SortPopover active={activeSort.id} onChange={(s) => setFilters((f) => ({ ...f, sort_by: s.sort_by, reverse: s.reverse }))} />
            </SectionBar>

            {search.isError ? (
                <div className="px-5 py-16 text-center text-[12.5px] text-red-600">
                    Couldn’t load deals. Try again.
                </div>
            ) : !search.isPending && deals.length === 0 ? (
                <EmptyDeals hasFilters={total === 0 && hasAnyFilter(filters)} onClear={() => setFilters(EMPTY_DEAL_SEARCH)} />
            ) : (
                <div className="flex-1 min-h-0 overflow-auto">
                    <table className="w-full border-collapse">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <Th className="text-left">Deal</Th>
                                <Th className="text-left hidden md:table-cell">Contact</Th>
                                <Th className="text-left hidden md:table-cell">Pipeline</Th>
                                <Th className="text-left">Stage</Th>
                                <Th className="text-right">Value</Th>
                                <Th className="text-left hidden md:table-cell">Status</Th>
                                <Th className="text-left hidden md:table-cell">Close</Th>
                            </tr>
                        </thead>
                        <tbody>
                            {search.isPending
                                ? Array.from({ length: 8 }).map((_, i) => <SkeletonRow key={i} />)
                                : deals.map((d) => (
                                      <DealRow
                                          key={d.id}
                                          deal={d}
                                          pipelineName={pipelineName.get(d.pipeline_id)}
                                          onOpen={() => onOpenDeal(d)}
                                      />
                                  ))}
                        </tbody>
                    </table>

                    <div className="px-5 py-3 flex items-center justify-center gap-3 border-t border-slate-200/60">
                        {search.hasNextPage ? (
                            <button
                                type="button"
                                onClick={() => search.fetchNextPage()}
                                disabled={search.isFetchingNextPage}
                                className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {search.isFetchingNextPage ? (
                                    <>
                                        <Loader2Icon className="w-3 h-3 animate-spin" />
                                        Loading…
                                    </>
                                ) : (
                                    <>
                                        <PlusIcon className="w-3 h-3" />
                                        Load more
                                    </>
                                )}
                            </button>
                        ) : null}
                        <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                            {search.hasNextPage
                                ? `${deals.length} of ${total} loaded`
                                : `${total} ${total === 1 ? "deal" : "deals"}`}
                        </span>
                    </div>
                </div>
            )}
        </>
    );
}

function DealRow({
    deal,
    pipelineName,
    onOpen,
}: {
    deal: Deal;
    pipelineName?: string;
    onOpen: () => void;
}) {
    const status = STATUS_STYLE[deal.status];
    const contactLabel = deal.contact
        ? [deal.contact.first_name, deal.contact.last_name].filter(Boolean).join(" ").trim() ||
          deal.contact.email
        : "";

    return (
        <tr
            onClick={onOpen}
            className="group h-11 border-b border-slate-200/60 hover:bg-slate-50/80 cursor-pointer transition-colors"
        >
            <td className="px-3 max-w-0">
                <div className="text-[12.5px] font-medium text-slate-900 truncate">{deal.name}</div>
            </td>
            <td className="px-3 max-w-0 hidden md:table-cell">
                {contactLabel ? (
                    <div className="flex items-center gap-1.5 text-[11.5px] text-slate-500 truncate">
                        <UserIcon className="w-3 h-3 shrink-0 text-slate-400" />
                        <span className="truncate">{contactLabel}</span>
                    </div>
                ) : (
                    <span className="text-slate-300 text-[11.5px]">—</span>
                )}
            </td>
            <td className="px-3 whitespace-nowrap hidden md:table-cell">
                <span className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-500">
                    <GitBranchIcon className="w-3 h-3 text-slate-400" />
                    {pipelineName ?? "—"}
                </span>
            </td>
            <td className="px-3 whitespace-nowrap">
                {deal.stage ? (
                    <span className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-600">
                        <span
                            className="size-1.5 rounded-full shrink-0"
                            style={{ backgroundColor: deal.stage.color || "#94a3b8" }}
                        />
                        {deal.stage.name}
                    </span>
                ) : (
                    <span className="text-slate-300 text-[11.5px]">—</span>
                )}
            </td>
            <td className="px-3 text-right whitespace-nowrap">
                {deal.value != null ? (
                    <span className="font-mono text-[11.5px] text-emerald-700 tabular-nums">
                        {money(deal.value, deal.currency)}
                    </span>
                ) : (
                    <span className="text-slate-300">—</span>
                )}
            </td>
            <td className="px-3 whitespace-nowrap hidden md:table-cell">
                <span className={`inline-flex items-center gap-1.5 text-[11px] font-medium ${status.cls}`}>
                    <span className={`size-1.5 rounded-full ${status.dot}`} />
                    {status.label}
                </span>
            </td>
            <td className="px-3 whitespace-nowrap hidden md:table-cell">
                {deal.expected_close_date ? (
                    <span className="inline-flex items-center gap-1 font-mono text-[11px] text-slate-400 tabular-nums">
                        <CalendarIcon className="w-2.5 h-2.5" />
                        {fmtDate(deal.expected_close_date)}
                    </span>
                ) : (
                    <span className="text-slate-300 text-[11.5px]">—</span>
                )}
            </td>
        </tr>
    );
}

function PipelineFacet({
    pipelines,
    selected,
    onChange,
}: {
    pipelines: Pipeline[];
    selected: string[];
    onChange: (ids: string[]) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const label = selected.length === 0 ? "All pipelines" : `${selected.length} pipeline${selected.length === 1 ? "" : "s"}`;

    function toggle(id: string) {
        onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        selected.length
                            ? "border-sky-300 bg-sky-50 text-sky-700"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <GitBranchIcon className="w-3 h-3" />
                    {label}
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200} className="max-h-64 overflow-y-auto">
                {pipelines.length === 0 ? (
                    <div className="px-2 py-1.5 text-[11.5px] text-slate-400">No pipelines yet</div>
                ) : (
                    pipelines.map((p) => (
                        <PopoverMenuItem
                            key={p.id}
                            onSelect={() => toggle(p.id)}
                            selected={selected.includes(p.id)}
                            closeOnSelect={false}
                        >
                            {p.name}
                        </PopoverMenuItem>
                    ))
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function SortPopover({
    active,
    onChange,
}: {
    active: string;
    onChange: (s: (typeof SORTS)[number]) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = SORTS.find((s) => s.id === active) ?? SORTS[0];
    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                >
                    <ArrowUpDownIcon className="w-3 h-3" />
                    {cur.label}
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={180}>
                {SORTS.map((s) => (
                    <PopoverMenuItem key={s.id} onSelect={() => onChange(s)} selected={s.id === active}>
                        {s.label}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function FilterPopover({
    filters,
    onChange,
    activeCount,
}: {
    filters: SearchDeals;
    onChange: React.Dispatch<React.SetStateAction<SearchDeals>>;
    activeCount: number;
}) {
    const [open, setOpen] = React.useState(false);

    const setNum = (key: "min_value" | "max_value", v: string) =>
        onChange((f) => ({ ...f, [key]: v.trim() === "" ? undefined : Number(v) }));
    const setDate = (key: "close_after" | "close_before", v: string) =>
        onChange((f) => ({ ...f, [key]: v ? new Date(v).toISOString() : undefined }));

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        activeCount
                            ? "border-sky-300 bg-sky-50 text-sky-700"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <FilterIcon className="w-3 h-3" />
                    Filters
                    {activeCount > 0 && (
                        <span className="size-4 rounded-full bg-sky-600 text-white text-[9.5px] inline-flex items-center justify-center tabular-nums">
                            {activeCount}
                        </span>
                    )}
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={260} className="p-3">
                <div className="space-y-3">
                    <div>
                        <Label>Value range</Label>
                        <div className="flex items-center gap-1.5">
                            <TextInput
                                value={filters.min_value != null ? String(filters.min_value) : ""}
                                onChange={(v) => setNum("min_value", v)}
                                placeholder="Min"
                                className="w-full"
                            />
                            <span className="text-slate-300">–</span>
                            <TextInput
                                value={filters.max_value != null ? String(filters.max_value) : ""}
                                onChange={(v) => setNum("max_value", v)}
                                placeholder="Max"
                                className="w-full"
                            />
                        </div>
                    </div>
                    <div>
                        <Label>Expected close</Label>
                        <div className="flex items-center gap-1.5">
                            <TextInput
                                value={filters.close_after ? String(filters.close_after).split("T")[0] : ""}
                                onChange={(v) => setDate("close_after", v)}
                                type="date"
                                className="w-full"
                            />
                            <span className="text-slate-300">–</span>
                            <TextInput
                                value={filters.close_before ? String(filters.close_before).split("T")[0] : ""}
                                onChange={(v) => setDate("close_before", v)}
                                type="date"
                                className="w-full"
                            />
                        </div>
                    </div>
                    <div className="flex items-center justify-between pt-1 border-t border-slate-100">
                        <button
                            type="button"
                            onClick={() =>
                                onChange((f) => ({
                                    ...f,
                                    min_value: undefined,
                                    max_value: undefined,
                                    close_after: undefined,
                                    close_before: undefined,
                                }))
                            }
                            className="text-[11.5px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1"
                        >
                            <XIcon className="w-3 h-3" />
                            Clear
                        </button>
                        <button
                            type="button"
                            onClick={() => setOpen(false)}
                            className="h-6 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium transition-colors"
                        >
                            Done
                        </button>
                    </div>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function EmptyDeals({ hasFilters, onClear }: { hasFilters: boolean; onClear: () => void }) {
    return (
        <div className="px-5 py-16 text-center">
            <div className="mx-auto size-9 rounded-md bg-slate-50 border border-slate-200 flex items-center justify-center mb-3">
                <CircleDollarSignIcon className="w-4 h-4 text-slate-400" />
            </div>
            <p className="text-[12.5px] text-slate-700 font-medium mb-1">
                {hasFilters ? "No deals match these filters" : "No deals yet"}
            </p>
            <p className="text-[11.5px] text-slate-400 mb-4 max-w-[40ch] mx-auto leading-relaxed">
                {hasFilters
                    ? "Try widening or clearing the filters to see more."
                    : "Deals you create across any pipeline show up here: searchable, sortable, and totalled across every pipeline."}
            </p>
            {hasFilters && (
                <button
                    type="button"
                    onClick={onClear}
                    className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                >
                    <XIcon className="w-3 h-3" />
                    Clear filters
                </button>
            )}
        </div>
    );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <th
            className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] ${className ?? ""}`}
        >
            {children}
        </th>
    );
}

function SkeletonRow() {
    return (
        <tr className="h-11 border-b border-slate-200/60">
            {Array.from({ length: 7 }).map((_, i) => (
                <td key={i} className={`px-3 ${[1, 2, 5, 6].includes(i) ? "hidden md:table-cell" : ""}`}>
                    <div className="h-3 bg-slate-100 rounded animate-pulse" style={{ width: `${50 + ((i * 13) % 40)}%` }} />
                </td>
            ))}
        </tr>
    );
}

function hasAnyFilter(f: SearchDeals): boolean {
    return (
        f.query.trim() !== "" ||
        f.statuses.length > 0 ||
        f.pipeline_ids.length > 0 ||
        f.stage_ids.length > 0 ||
        f.assigned_to.length > 0 ||
        f.campaign_ids.length > 0 ||
        f.min_value != null ||
        f.max_value != null ||
        !!f.close_after ||
        !!f.close_before
    );
}

function money(n: number | undefined, currency = "USD") {
    if (n == null) return "—";
    try {
        return new Intl.NumberFormat("en-US", {
            style: "currency",
            currency: currency || "USD",
            maximumFractionDigits: 0,
        }).format(n);
    } catch {
        return `$${Math.round(n).toLocaleString()}`;
    }
}

function fmtDate(d: string | undefined) {
    if (!d) return "—";
    try {
        return new Date(d).toLocaleDateString("en-US", { month: "short", day: "numeric" });
    } catch {
        return "—";
    }
}
