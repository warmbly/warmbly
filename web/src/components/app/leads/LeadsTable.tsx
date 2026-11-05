import { useEffect, useRef } from "react";
import { ArrowDownWideNarrowIcon, CheckIcon, GlobeIcon, Loader2Icon, MailIcon, PhoneIcon, SearchXIcon, StarIcon, UserCheckIcon, UsersIcon } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { Lead, LeadSort } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";
import { EmailBadge, ScoreBar, StatusBadge } from "./badges";

const SORTS: { id: LeadSort; label: string }[] = [
    { id: "score", label: "Best leads first" },
    { id: "recent", label: "Most recently found" },
    { id: "rating", label: "Highest rated" },
    { id: "reviews", label: "Most reviewed" },
    { id: "name", label: "Name A to Z" },
];

export default function LeadsTable({
    leads,
    total,
    loading,
    hasMore,
    loadingMore,
    onMore,
    sort,
    onSort,
    selected,
    allMatching,
    onToggle,
    onTogglePage,
    onSelectAllMatching,
    activeId,
    onOpen,
    onHover,
}: {
    leads: Lead[];
    total: number;
    loading: boolean;
    hasMore: boolean;
    loadingMore: boolean;
    onMore: () => void;
    sort: LeadSort;
    onSort: (s: LeadSort) => void;
    selected: Set<string>;
    allMatching: boolean;
    onToggle: (id: string, range: boolean) => void;
    onTogglePage: (on: boolean) => void;
    onSelectAllMatching: () => void;
    activeId: string | null;
    onOpen: (id: string) => void;
    onHover: (id: string | null) => void;
}) {
    const sentinel = useRef<HTMLDivElement>(null);
    useEffect(() => {
        const el = sentinel.current;
        if (!el || !hasMore) return;
        const io = new IntersectionObserver((e) => e[0]?.isIntersecting && !loadingMore && onMore(), { rootMargin: "240px" });
        io.observe(el);
        return () => io.disconnect();
    }, [hasMore, loadingMore, onMore]);

    const pageAll = leads.length > 0 && leads.every((l) => selected.has(l.id));
    const pageSome = leads.some((l) => selected.has(l.id));

    return (
        <div className="flex h-full min-h-0 flex-col">
            <div className="shrink-0 h-9 px-3 flex items-center gap-2 border-b border-slate-200 bg-white">
                <Checkbox
                    aria-label="Select every lead on screen"
                    checked={pageAll || allMatching}
                    ref={(el) => {
                        if (el) el.indeterminate = !pageAll && pageSome && !allMatching;
                    }}
                    onChange={(e) => onTogglePage(e.target.checked)}
                />
                <span className="text-[12px] text-slate-500 tabular-nums">
                    {loading ? "Loading…" : `${total.toLocaleString()} lead${total === 1 ? "" : "s"}`}
                </span>
                {pageAll && !allMatching && total > leads.length && (
                    <button type="button" onClick={onSelectAllMatching} className="text-[11.5px] text-sky-700 hover:underline">
                        Select all {Math.min(total, 5000).toLocaleString()} matching
                    </button>
                )}
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            className="ml-auto h-6 px-2 rounded-md text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5"
                        >
                            <ArrowDownWideNarrowIcon className="size-3.5" />
                            {SORTS.find((s) => s.id === sort)?.label}
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={190}>
                        {SORTS.map((s) => (
                            <PopoverMenuItem key={s.id} onSelect={() => onSort(s.id)} trailing={s.id === sort ? <CheckIcon className="size-3.5 text-sky-600" /> : undefined}>
                                {s.label}
                            </PopoverMenuItem>
                        ))}
                    </PopoverMenuContent>
                </PopoverMenu>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto">
                {loading ? (
                    <div className="divide-y divide-slate-100">
                        {Array.from({ length: 8 }).map((_, i) => (
                            <div key={i} className="h-[58px] px-3 flex items-center gap-3">
                                <div className="size-4 rounded bg-slate-100" />
                                <div className="flex-1 space-y-1.5">
                                    <div className="h-3 w-1/3 rounded bg-slate-100 animate-pulse" />
                                    <div className="h-2.5 w-1/2 rounded bg-slate-100 animate-pulse" />
                                </div>
                            </div>
                        ))}
                    </div>
                ) : leads.length === 0 ? (
                    <div className="px-6 py-14 text-center">
                        <SearchXIcon className="mx-auto size-6 text-slate-300" />
                        <p className="mt-2 text-[12.5px] font-medium text-slate-900">No leads here yet</p>
                        <p className="mt-0.5 text-[11.5px] text-slate-400">Search an area, or loosen the filters on the left.</p>
                    </div>
                ) : (
                    <ul className="divide-y divide-slate-100">
                        {leads.map((l) => (
                            <Row
                                key={l.id}
                                lead={l}
                                checked={allMatching || selected.has(l.id)}
                                active={l.id === activeId}
                                onToggle={(range) => onToggle(l.id, range)}
                                onOpen={() => onOpen(l.id)}
                                onHover={onHover}
                            />
                        ))}
                    </ul>
                )}
                <div ref={sentinel} />
                {loadingMore && (
                    <div className="py-3 flex justify-center">
                        <Loader2Icon className="size-4 animate-spin text-slate-400" />
                    </div>
                )}
            </div>
        </div>
    );
}

function Row({
    lead,
    checked,
    active,
    onToggle,
    onOpen,
    onHover,
}: {
    lead: Lead;
    checked: boolean;
    active: boolean;
    onToggle: (range: boolean) => void;
    onOpen: () => void;
    onHover: (id: string | null) => void;
}) {
    return (
        <li
            onClick={onOpen}
            onMouseEnter={() => onHover(lead.id)}
            onMouseLeave={() => onHover(null)}
            data-active={active || undefined}
            className={cn("group px-3 py-2 flex items-center gap-3 cursor-pointer transition-colors", active ? "bg-sky-50/70" : "hover:bg-slate-50")}
        >
            <span onClick={(e) => e.stopPropagation()} className="shrink-0">
                <Checkbox
                    aria-label={`Select ${lead.name}`}
                    checked={checked}
                    onChange={() => undefined}
                    onClick={(e) => onToggle((e as React.MouseEvent).shiftKey)}
                />
            </span>
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                    <span className="truncate text-[12.5px] font-medium text-slate-900">{lead.name}</span>
                    {lead.contact_id && (
                        <span title="Already a contact" className="shrink-0 text-emerald-600">
                            <UserCheckIcon className="size-3.5" />
                        </span>
                    )}
                    {lead.business_status === "closed" && <span className="shrink-0 rounded bg-rose-50 px-1 text-[10px] text-rose-700">Closed</span>}
                    {lead.people_count > 0 && (
                        <span title={`${lead.people_count} ${lead.people_count === 1 ? "person" : "people"} found`} className="shrink-0 inline-flex items-center gap-0.5 rounded bg-violet-50 px-1 text-[10px] font-medium tabular-nums text-violet-700">
                            <UsersIcon className="size-2.5" />
                            {lead.people_count}
                        </span>
                    )}
                </div>
                <div className="truncate text-[11.5px] text-slate-500">
                    {[lead.category, lead.city || lead.address].filter(Boolean).join(" · ")}
                </div>
            </div>
            <div className="hidden lg:flex w-[210px] shrink-0 flex-col gap-0.5">
                {lead.email ? (
                    <EmailBadge lead={lead} />
                ) : (
                    <span className="inline-flex items-center gap-1 text-[11.5px] text-slate-400">
                        <MailIcon className="size-3" />
                        {lead.enrichment_status === "queued" ? "Looking…" : lead.website ? "Not found yet" : "No email"}
                    </span>
                )}
                {lead.phone && (
                    <span className="inline-flex items-center gap-1 truncate text-[11.5px] text-slate-500">
                        <PhoneIcon className="size-3 shrink-0" />
                        {lead.phone}
                    </span>
                )}
            </div>
            <div className="hidden xl:flex w-[150px] shrink-0 items-center gap-1 text-[11.5px] text-slate-500 truncate">
                {lead.domain ? (
                    <>
                        <GlobeIcon className="size-3 shrink-0" />
                        <span className="truncate">{lead.domain}</span>
                    </>
                ) : (
                    <span className="text-slate-400">No website</span>
                )}
            </div>
            <div className="hidden sm:flex w-[64px] shrink-0 items-center gap-1 text-[11.5px] text-slate-600 tabular-nums">
                {lead.rating != null ? (
                    <>
                        <StarIcon className="size-3 fill-amber-400 text-amber-400" />
                        {lead.rating.toFixed(1)}
                        {lead.review_count != null && <span className="text-slate-400">({compact(lead.review_count)})</span>}
                    </>
                ) : (
                    <span className="text-slate-300">-</span>
                )}
            </div>
            <div className="w-[56px] shrink-0">
                <ScoreBar score={lead.score} />
            </div>
            <div className="hidden md:block w-[78px] shrink-0">
                <StatusBadge status={lead.status} />
            </div>
        </li>
    );
}

function compact(n: number): string {
    return n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : String(n);
}
