// The checkout's search: live results as you type, each one added to the order with one press. A domain the
// workspace already owns is a quiet link that opens in place, never a tab.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { CheckIcon, ChevronDownIcon, GlobeIcon, Loader2Icon, PlusIcon, SearchIcon, SparklesIcon, XIcon } from "lucide-react";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useDomainSearch } from "@/lib/api/hooks/app/store/useStore";
import type { DomainSearchResult, StoreDomain } from "@/lib/api/models/app/store/Store";
import { storeErrorMessage } from "@/lib/buy/errors";
import { fmtMicros, fmtYearly } from "@/lib/buy/money";
import { EASE, HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";
import { cn } from "@/lib/utils";
import { SearchIllustration } from "./Illustrations";
import OwnDomainPanel from "./OwnDomainPanel";

const REASON: Record<string, string> = {
    registered: "Taken",
    premium: "Premium name",
    reserved: "Reserved",
    tld_unsupported: "Not offered",
};

// Results past this sit behind "Show all", so the order below stays in reach.
const FIRST_RESULTS = 6;

/** What the search is asked for: lowercase, no scheme, no spaces at the ends. */
function searchTerm(raw: string): string {
    return raw
        .trim()
        .toLowerCase()
        .replace(/^https?:\/\//, "")
        .replace(/^www\./, "")
        .replace(/\/.*$/, "");
}

export default function DomainSearch({
    query,
    onQuery,
    inOrder,
    onToggle,
    onToggleOwned,
    atCost,
    hasOrder,
}: {
    query: string;
    onQuery: (q: string) => void;
    inOrder: (domain: string) => boolean;
    onToggle: (r: DomainSearchResult) => void;
    onToggleOwned: (d: StoreDomain) => void;
    atCost: boolean;
    /** The order has domains, so the empty-search welcome steps aside for it. */
    hasOrder: boolean;
}) {
    const reduced = useReducedMotion();
    const [own, setOwn] = React.useState(false);
    const [showAll, setShowAll] = React.useState(false);
    const term = searchTerm(query);
    const debounced = useDebouncedValue(term, 280);
    const search = useDomainSearch(debounced);
    const results = term.length >= 2 ? search.data?.data : undefined;
    const settling = term.length >= 2 && (debounced !== term || search.isFetching);
    const firstLoad = term.length >= 2 && !results && (settling || search.isPending);
    const available = results?.filter((r) => r.available).length ?? 0;
    React.useEffect(() => setShowAll(false), [debounced]);
    const shown = results && !showAll ? results.slice(0, FIRST_RESULTS) : results;

    return (
        <div>
            <label className="group relative flex items-center h-12 rounded-xl border border-slate-200 bg-white px-3.5 gap-3 shadow-[0_1px_2px_rgba(15,23,42,0.05)] focus-within:border-sky-400 focus-within:ring-4 focus-within:ring-sky-100 transition-[border-color,box-shadow]">
                <SearchIcon className="w-[18px] h-[18px] text-slate-400 group-focus-within:text-sky-600 transition-colors shrink-0" />
                <span className="sr-only">Search for a domain</span>
                <input
                    autoFocus={!hasOrder}
                    value={query}
                    onChange={(e) => onQuery(e.target.value)}
                    placeholder="Your brand, like acme"
                    autoComplete="off"
                    spellCheck={false}
                    autoCapitalize="none"
                    className="flex-1 min-w-0 h-full bg-transparent outline-none text-[16px] md:text-[15px] text-slate-900 placeholder:text-slate-400"
                />
                <AnimatePresence initial={false}>
                    {settling && (
                        <motion.span key="spin" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
                            <Loader2Icon className="w-4 h-4 text-slate-400 animate-spin" />
                        </motion.span>
                    )}
                </AnimatePresence>
                {query && (
                    <button
                        type="button"
                        onClick={() => onQuery("")}
                        aria-label="Clear search"
                        className="size-7 rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                )}
            </label>
            <button
                type="button"
                onClick={() => setOwn((v) => !v)}
                aria-expanded={own}
                className="mt-2 inline-flex items-center gap-1 rounded text-[12px] text-slate-500 hover:text-slate-900 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
            >
                <GlobeIcon className="w-3 h-3" /> Use a domain I already own
                <ChevronDownIcon className={cn("w-3 h-3 transition-transform", own && "rotate-180")} />
            </button>

            <AnimatePresence initial={false}>
                {own && (
                    <motion.div
                        key="own"
                        initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        transition={{ duration: 0.24, ease: EASE }}
                        className="overflow-hidden"
                    >
                        <p className="mt-3 text-[12px] text-slate-500 max-w-[60ch] leading-relaxed">
                            Connect a domain registered elsewhere and buy mailboxes on it. Warmbly sets up mail and authentication once it is connected.
                        </p>
                        <OwnDomainPanel inCart={inOrder} onAdd={onToggleOwned} onBack={() => setOwn(false)} />
                    </motion.div>
                )}
            </AnimatePresence>

            <div className="mt-3" aria-live="polite" aria-busy={firstLoad || undefined}>
                {term.length < 2 ? (
                    hasOrder ? null : (
                        <div className="pt-6 pb-4 flex flex-col items-center text-center">
                            <SearchIllustration className="w-[200px] max-w-full h-auto" />
                            <p className="mt-2 text-[13px] text-slate-800 font-medium">Search by your brand name</p>
                            <p className="mt-1 text-[12px] text-slate-500 max-w-[44ch] leading-relaxed">
                                You get the exact match when it is free, plus close variations like getacme.com or acmehq.com.
                            </p>
                        </div>
                    )
                ) : firstLoad ? (
                    <ResultsSkeleton />
                ) : search.isError && !results ? (
                    <p className="py-8 text-center text-[12.5px] text-slate-500">
                        {storeErrorMessage(search.error)}{" "}
                        <button type="button" onClick={() => void search.refetch()} className="text-sky-700 hover:text-sky-900 font-medium">
                            Try again
                        </button>
                    </p>
                ) : results && results.length === 0 ? (
                    <p className="py-8 text-center text-[12.5px] text-slate-500">Nothing came up for {term}. Try a shorter word.</p>
                ) : shown ? (
                    <>
                        <motion.ul
                            key={search.data?.query ?? debounced}
                            initial="hidden"
                            animate="show"
                            variants={{ hidden: {}, show: { transition: { staggerChildren: reduced ? 0 : 0.04, delayChildren: reduced ? 0 : 0.03 } } }}
                            // Hairline dividers come from the gap, so the rows read the same in one column or two.
                            className={cn("grid gap-px rounded-xl border border-slate-200 overflow-hidden bg-slate-100 transition-opacity", shown.length > 1 && "2xl:grid-cols-2", settling && "opacity-70")}
                        >
                            {shown.map((r) => (
                                <ResultRow key={r.domain} r={r} added={inOrder(r.domain)} onToggle={() => onToggle(r)} atCost={atCost} />
                            ))}
                            {shown.length > 1 && shown.length % 2 === 1 && <li aria-hidden="true" className="hidden 2xl:block bg-white" />}
                        </motion.ul>
                        <div className="mt-2 flex flex-wrap items-start gap-x-4 gap-y-1.5">
                            {results && results.length > FIRST_RESULTS && !showAll && (
                                <button
                                    type="button"
                                    onClick={() => setShowAll(true)}
                                    className="inline-flex items-center gap-1 rounded text-[12px] font-medium text-sky-700 hover:text-sky-900 outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                                >
                                    Show all {results.length} <ChevronDownIcon className="w-3 h-3" />
                                </button>
                            )}
                            {available > 1 && (
                                <p className="flex items-start gap-1.5 text-[11.5px] text-slate-500 min-w-0 flex-1">
                                    <SparklesIcon className="w-3.5 h-3.5 text-sky-500 mt-px shrink-0" />
                                    Two or three domains let each mailbox send less while you reach the same number of people.
                                </p>
                            )}
                        </div>
                    </>
                ) : null}
            </div>
        </div>
    );
}

function ResultRow({ r, added, onToggle, atCost }: { r: DomainSearchResult; added: boolean; onToggle: () => void; atCost: boolean }) {
    const reduced = useReducedMotion();
    const dot = r.tld.length + 1;
    const name = r.display_name || r.domain;
    const stem = name.length > dot ? name.slice(0, -dot) : name;
    const tld = name.length > dot ? name.slice(-dot) : "";
    return (
        <motion.li
            variants={{ hidden: { opacity: 0, y: reduced ? 0 : 8, scale: reduced ? 1 : 0.99 }, show: { opacity: 1, y: 0, scale: 1 } }}
            transition={{ duration: 0.26, ease: EASE }}
            className="bg-white min-w-0"
        >
            <div className={cn("h-full px-3 sm:px-4 py-2.5 flex items-center gap-3 transition-colors", added ? "bg-sky-50/60" : r.available ? "hover:bg-slate-50/70" : "")}>
                <span className={cn("size-2 rounded-full shrink-0", r.available ? "bg-emerald-500" : "bg-slate-300")} title={r.available ? "Available" : (REASON[r.reason ?? ""] ?? "Unavailable")} />
                <div className="min-w-0 flex-1">
                    {/* The full name always shows: a long one wraps, and the TLD never splits off or truncates. */}
                    <div className={cn("text-[13.5px] leading-snug [overflow-wrap:anywhere]", r.available ? "text-slate-900" : "text-slate-400 line-through decoration-slate-300")} title={name}>
                        <span className="font-semibold">{stem}</span>
                        <span className={cn("whitespace-nowrap", r.available ? "text-slate-500" : undefined)}>{tld}</span>
                    </div>
                    <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 min-w-0 text-[11px] text-slate-500">
                        {r.exact && (
                            <span className="shrink-0 h-4 px-1.5 rounded-full bg-sky-50 border border-sky-100 text-sky-700 text-[10px] font-medium inline-flex items-center">
                                Exact match
                            </span>
                        )}
                        <span>
                            {r.available
                                ? `${(r.years ?? 1) > 1 ? `First ${r.years} years, then ` : "Renews at "}${fmtYearly(r.renew_micros)}${atCost ? " · at cost" : ""}`
                                : (REASON[r.reason ?? ""] ?? "Unavailable")}
                        </span>
                    </div>
                </div>
                {r.available && (
                    <>
                        <span className="hidden sm:inline text-[12.5px] text-slate-900 tabular-nums font-medium">{fmtMicros(r.price_micros)}</span>
                        <AddButton added={added} onClick={onToggle} label={r.domain} price={fmtMicros(r.price_micros)} />
                    </>
                )}
            </div>
        </motion.li>
    );
}

/** "Add" that morphs into a check once the domain is in the order; pressing it again takes it out. */
function AddButton({ added, onClick, label, price }: { added: boolean; onClick: () => void; label: string; price: string }) {
    const reduced = useReducedMotion();
    return (
        <motion.button
            type="button"
            layout={!reduced}
            onClick={onClick}
            aria-pressed={added}
            aria-label={added ? `Remove ${label} from the order` : `Add ${label} for ${price}`}
            whileHover={reduced ? undefined : HOVER_LIFT}
            whileTap={reduced ? undefined : { scale: 0.94 }}
            transition={PRESS_SPRING}
            className={cn(
                "h-7 rounded-full inline-flex items-center justify-center gap-1 text-[12px] font-medium shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                added ? "w-7 bg-sky-600 text-white hover:bg-sky-700" : "px-2.5 border border-slate-200 bg-white text-slate-800 hover:border-sky-300 hover:text-sky-800",
            )}
        >
            <AnimatePresence initial={false} mode="popLayout">
                {added ? (
                    <motion.span
                        key="on"
                        initial={reduced ? false : { scale: 0.3, rotate: -45, opacity: 0 }}
                        animate={{ scale: 1, rotate: 0, opacity: 1 }}
                        exit={{ scale: 0.3, opacity: 0 }}
                        transition={{ type: "spring", stiffness: 600, damping: 20 }}
                        className="inline-flex"
                    >
                        <CheckIcon className="w-3.5 h-3.5" strokeWidth={2.75} />
                    </motion.span>
                ) : (
                    <motion.span
                        key="off"
                        initial={reduced ? false : { opacity: 0, scale: 0.8 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.8 }}
                        transition={{ duration: 0.12 }}
                        className="inline-flex items-center gap-1"
                    >
                        <PlusIcon className="w-3 h-3" />
                        <span className="sm:hidden tabular-nums">{price}</span>
                        <span className="hidden sm:inline">Add</span>
                    </motion.span>
                )}
            </AnimatePresence>
        </motion.button>
    );
}

function ResultsSkeleton() {
    return (
        <div className="grid gap-px 2xl:grid-cols-2 rounded-xl border border-slate-200 overflow-hidden bg-slate-100" aria-hidden="true">
            {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="bg-white px-4 py-3 flex items-center gap-3">
                    <span className="size-2 rounded-full skeleton-shimmer" />
                    <div className="flex-1 space-y-1.5">
                        <div className="h-3 rounded skeleton-shimmer" style={{ width: `${34 + ((i * 13) % 30)}%` }} />
                        <div className="h-2.5 w-28 rounded skeleton-shimmer" />
                    </div>
                    <div className="hidden sm:block h-3 w-12 rounded skeleton-shimmer" />
                    <div className="h-7 w-14 rounded-full skeleton-shimmer" />
                </div>
            ))}
        </div>
    );
}
