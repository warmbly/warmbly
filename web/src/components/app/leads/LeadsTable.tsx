import { useEffect, useRef, useState } from "react";
import { ArrowDownWideNarrowIcon, CheckIcon, GlobeIcon, Loader2Icon, MailXIcon, PhoneIcon, RotateCwIcon, SearchXIcon, SparklesIcon, StarIcon, UserCheckIcon, UsersIcon } from "lucide-react";
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
    error,
    onRetry,
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
    banner,
}: {
    leads: Lead[];
    total: number;
    loading: boolean;
    // Set when the list failed to load and there is nothing to show.
    error?: string | null;
    onRetry?: () => void;
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
    // Search and website-run progress, shown above the rows.
    banner?: React.ReactNode;
}) {
    const sentinel = useRef<HTMLDivElement>(null);
    useEffect(() => {
        const el = sentinel.current;
        if (!el || !hasMore) return;
        const io = new IntersectionObserver((e) => e[0]?.isIntersecting && !loadingMore && onMore(), { rootMargin: "240px" });
        io.observe(el);
        return () => io.disconnect();
    }, [hasMore, loadingMore, onMore]);

    // Rows that just finished being read flash once as they turn over.
    const wasQueued = useRef(new Set<string>());
    const [justDone, setJustDone] = useState<Set<string>>(new Set());
    useEffect(() => {
        const done: string[] = [];
        const queued = new Set<string>();
        for (const l of leads) {
            if (l.enrichment_status === "queued") queued.add(l.id);
            else if (wasQueued.current.has(l.id)) done.push(l.id);
        }
        wasQueued.current = queued;
        if (!done.length) return;
        setJustDone((cur) => new Set([...cur, ...done]));
        // Not cleared on re-render: a poll landing inside the flash would strand the highlight.
        setTimeout(() => setJustDone((cur) => new Set([...cur].filter((id) => !done.includes(id)))), 2_400);
    }, [leads]);

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
            {banner}
            {!loading && leads.length > 0 && (
                <div className="shrink-0 h-7 pl-10 pr-3 flex items-center gap-3 border-b border-slate-100 bg-slate-50/60 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                    <span className="flex-1">Business</span>
                    <span className="hidden lg:block w-[210px] shrink-0">Contact</span>
                    <span className="hidden xl:block w-[150px] shrink-0">Website</span>
                    <span className="hidden sm:block w-[64px] shrink-0">Rating</span>
                    <span className="w-[56px] shrink-0">Score</span>
                    <span className="hidden md:block w-[78px] shrink-0">Status</span>
                </div>
            )}
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
                ) : error ? (
                    <div className="px-6 py-14 text-center">
                        <SearchXIcon className="mx-auto size-6 text-slate-300" />
                        <p className="mt-2 text-[12.5px] font-medium text-slate-900">Leads did not load</p>
                        <p className="mt-0.5 text-[11.5px] text-slate-400">{error}</p>
                        {onRetry && (
                            <button
                                type="button"
                                onClick={onRetry}
                                className="mt-3 h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 hover:text-slate-900 inline-flex items-center gap-1.5"
                            >
                                <RotateCwIcon className="size-3.5" />
                                Try again
                            </button>
                        )}
                    </div>
                ) : leads.length === 0 ? (
                    <div className="px-6 py-14 text-center">
                        <SearchXIcon className="mx-auto size-6 text-slate-300" />
                        <p className="mt-2 text-[12.5px] font-medium text-slate-900">No leads here yet</p>
                        <p className="mt-0.5 text-[11.5px] text-slate-400">Search an area, or loosen the filters above.</p>
                    </div>
                ) : (
                    <ul className="divide-y divide-slate-100">
                        {leads.map((l) => (
                            <Row
                                key={l.id}
                                lead={l}
                                checked={allMatching || selected.has(l.id)}
                                active={l.id === activeId}
                                justDone={justDone.has(l.id)}
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
    justDone,
    onToggle,
    onOpen,
    onHover,
}: {
    lead: Lead;
    checked: boolean;
    active: boolean;
    justDone: boolean;
    onToggle: (range: boolean) => void;
    onOpen: () => void;
    onHover: (id: string | null) => void;
}) {
    const working = lead.enrichment_status === "queued";
    return (
        <li
            onClick={onOpen}
            onMouseEnter={() => onHover(lead.id)}
            onMouseLeave={() => onHover(null)}
            data-active={active || undefined}
            className={cn(
                "group relative px-3 py-2 min-h-[52px] flex items-center gap-3 cursor-pointer transition-colors duration-700",
                active ? "bg-sky-50/70" : justDone ? "bg-emerald-50/70" : working ? "bg-sky-50/30 hover:bg-sky-50/60" : "hover:bg-slate-50",
            )}
        >
            {working && (
                <>
                    <span aria-hidden className="absolute left-0 inset-y-0 w-0.5 bg-sky-500" />
                    <span aria-hidden className="absolute inset-x-0 bottom-0 h-px overflow-hidden">
                        <span className="absolute inset-y-0 w-1/3 bg-gradient-to-r from-transparent via-sky-400 to-transparent progress-sweep" />
                    </span>
                </>
            )}
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
                <ContactCell lead={lead} working={working} />
            </div>
            <div className="hidden xl:flex w-[150px] shrink-0 items-center gap-1 text-[11.5px] text-slate-600 min-w-0">
                {lead.domain && (
                    <>
                        <GlobeIcon className="size-3 shrink-0 text-slate-400" />
                        <span className="truncate">{lead.domain}</span>
                        {lead.site.found_website && (
                            <span title="Not in the listing: worked out from the name and confirmed on the site" className="shrink-0 text-sky-600">
                                <SparklesIcon className="size-3" />
                            </span>
                        )}
                    </>
                )}
            </div>
            <div className="hidden sm:flex w-[64px] shrink-0 items-center gap-1 text-[11.5px] text-slate-600 tabular-nums">
                {lead.rating != null && (
                    <>
                        <StarIcon className="size-3 fill-amber-400 text-amber-400" />
                        {lead.rating.toFixed(1)}
                        {lead.review_count != null && <span className="text-slate-400">({compact(lead.review_count)})</span>}
                    </>
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

// ContactCell says how to reach a lead, or what is happening to find out.
function ContactCell({ lead, working }: { lead: Lead; working: boolean }) {
    const phone = lead.phone && (
        <span className="inline-flex items-center gap-1 truncate text-[11.5px] text-slate-600 tabular-nums">
            <PhoneIcon className="size-3 shrink-0 text-slate-400" />
            {lead.phone}
        </span>
    );
    if (working) {
        return (
            <>
                <span className="inline-flex items-center gap-1.5 text-[11.5px]">
                    <span className="campaign-grid text-sky-600 shrink-0" aria-hidden />
                    <span className="text-shimmer font-medium text-sky-700">{lead.website ? "Reading the website" : "Looking for a website"}</span>
                </span>
                {phone}
            </>
        );
    }
    if (lead.email) {
        return (
            <>
                <EmailBadge lead={lead} />
                {phone}
            </>
        );
    }
    const note =
        lead.enrichment_status === "done"
            ? "No email on the site"
            : lead.enrichment_status === "no_website"
              ? "No website found"
              : lead.enrichment_status === "failed"
                ? "No info available"
                : null;
    return (
        <>
            {phone}
            {note && (
                <span className="inline-flex items-center gap-1 text-[11px] text-slate-400">
                    <MailXIcon className="size-3 shrink-0" />
                    {note}
                </span>
            )}
        </>
    );
}

function compact(n: number): string {
    return n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : String(n);
}
