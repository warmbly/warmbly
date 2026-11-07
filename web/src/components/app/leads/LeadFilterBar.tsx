import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, SearchIcon, SlidersHorizontalIcon, StarIcon, XIcon } from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import ScrollStrip from "@/components/ui/scroll-strip";
import type { LeadFacets } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";
import FilterPanel from "./FilterPanel";
import { EMPTY_REFINE, refineChips, refineCount, type ReachFilter, type Refine } from "./refine";

// The filters people reach for most sit on the bar as one-tap toggles; the
// rest open from "Filters" and show up as chips once they are on.
const QUICK: { key: string; label: string; on: (r: Refine) => boolean; flip: (r: Refine) => Refine; icon?: "star" }[] = [
    { key: "reach:email", label: "Has email", on: (r) => r.reach.includes("email"), flip: (r) => flipReach(r, "email") },
    { key: "reach:website", label: "Has website", on: (r) => r.reach.includes("website"), flip: (r) => flipReach(r, "website") },
    { key: "reach:phone", label: "Has phone", on: (r) => r.reach.includes("phone"), flip: (r) => flipReach(r, "phone") },
    { key: "rating", label: "4+", icon: "star", on: (r) => r.minRating === 4, flip: (r) => ({ ...r, minRating: r.minRating === 4 ? null : 4 }) },
];

function flipReach(r: Refine, id: ReachFilter): Refine {
    if (r.reach.includes(id)) return { ...r, reach: r.reach.filter((x) => x !== id) };
    const reach = [...r.reach, id].filter((x) => !(id === "website" && x === "no_website"));
    return { ...r, reach };
}

const spring = { type: "spring", stiffness: 520, damping: 34, mass: 0.6 } as const;

export default function LeadFilterBar({
    text,
    onText,
    value,
    onChange,
    facets,
}: {
    text: string;
    onText: (v: string) => void;
    value: Refine;
    onChange: (r: Refine) => void;
    facets?: LeadFacets;
}) {
    // A chip is hidden only while its toggle shows it; a 3+ rating still gets one.
    const quickKeys = new Set(QUICK.filter((q) => q.on(value)).map((q) => q.key));
    const chips = refineChips(value).filter((c) => !quickKeys.has(c.key));
    const count = refineCount(value);
    const anything = count > 0 || text.trim() !== "";

    return (
        <div className="shrink-0 h-11 pl-3 pr-2 flex items-center gap-2 border-b border-slate-200 bg-white">
            <div className="relative w-40 sm:w-56 shrink-0">
                <SearchIcon className="absolute left-2 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
                <input
                    value={text}
                    onChange={(e) => onText(e.target.value)}
                    onKeyDown={(e) => e.key === "Escape" && onText("")}
                    placeholder="Filter by name, email, domain"
                    aria-label="Filter leads"
                    className="h-7 w-full rounded-md border border-slate-200 bg-white pl-7 pr-6 text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none transition-[border-color,box-shadow] focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                <AnimatePresence>
                    {text && (
                        <motion.button
                            type="button"
                            aria-label="Clear filter text"
                            onClick={() => onText("")}
                            initial={{ opacity: 0, scale: 0.6 }}
                            animate={{ opacity: 1, scale: 1 }}
                            exit={{ opacity: 0, scale: 0.6 }}
                            transition={{ duration: 0.12 }}
                            className="absolute right-1 top-1/2 -translate-y-1/2 size-5 rounded text-slate-400 hover:text-slate-700 inline-flex items-center justify-center"
                        >
                            <XIcon className="size-3" />
                        </motion.button>
                    )}
                </AnimatePresence>
            </div>

            <ScrollStrip className="flex-1" innerClassName="gap-1.5 py-1">
                {QUICK.map((q) => {
                    const on = q.on(value);
                    return (
                        <motion.button
                            key={q.key}
                            type="button"
                            aria-pressed={on}
                            whileTap={{ scale: 0.94 }}
                            onClick={() => onChange(q.flip(value))}
                            className={cn(
                                "h-7 shrink-0 px-2.5 rounded-full border text-[12px] inline-flex items-center gap-1 transition-colors duration-150",
                                on ? "border-sky-300 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
                            )}
                        >
                            <AnimatePresence initial={false}>
                                {on && (
                                    <motion.span
                                        initial={{ width: 0, opacity: 0 }}
                                        animate={{ width: "auto", opacity: 1 }}
                                        exit={{ width: 0, opacity: 0 }}
                                        transition={{ duration: 0.16 }}
                                        className="inline-flex overflow-hidden"
                                    >
                                        <CheckIcon className="size-3" />
                                    </motion.span>
                                )}
                            </AnimatePresence>
                            {q.icon === "star" && <StarIcon className={cn("size-3", on ? "fill-amber-400 text-amber-400" : "text-slate-400")} />}
                            {q.label}
                        </motion.button>
                    );
                })}

                <AnimatePresence initial={false} mode="popLayout">
                    {chips.map((c) => (
                        <motion.span
                            key={c.key}
                            layout
                            initial={{ opacity: 0, scale: 0.85 }}
                            animate={{ opacity: 1, scale: 1 }}
                            exit={{ opacity: 0, scale: 0.85 }}
                            transition={spring}
                            className="h-7 shrink-0 pl-2.5 pr-1 rounded-full bg-slate-900 text-white text-[12px] inline-flex items-center gap-1"
                        >
                            {c.label}
                            <button
                                type="button"
                                aria-label={`Remove ${c.label}`}
                                onClick={() => onChange(c.clear(value))}
                                className="size-5 rounded-full text-white/60 hover:text-white hover:bg-white/15 inline-flex items-center justify-center"
                            >
                                <XIcon className="size-3" />
                            </button>
                        </motion.span>
                    ))}
                </AnimatePresence>
            </ScrollStrip>

            <AnimatePresence initial={false}>
                {anything && (
                    <motion.button
                        type="button"
                        onClick={() => {
                            onChange(EMPTY_REFINE);
                            onText("");
                        }}
                        initial={{ opacity: 0, x: 6 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: 6 }}
                        transition={{ duration: 0.15 }}
                        className="hidden sm:inline-flex h-7 shrink-0 px-2 rounded-md text-[12px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 items-center"
                    >
                        Clear
                    </motion.button>
                )}
            </AnimatePresence>

            <PopoverMenu align="end">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        className="h-7 shrink-0 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:border-slate-300 hover:text-slate-900 data-[state=open]:border-sky-300 data-[state=open]:bg-sky-50 data-[state=open]:text-sky-700 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <SlidersHorizontalIcon className="size-3.5" />
                        <span className="hidden sm:inline">Filters</span>
                        <AnimatePresence initial={false}>
                            {count > 0 && (
                                <motion.span
                                    key={count}
                                    initial={{ scale: 0.5, opacity: 0 }}
                                    animate={{ scale: 1, opacity: 1 }}
                                    exit={{ scale: 0.5, opacity: 0 }}
                                    transition={spring}
                                    className="min-w-4 rounded-full bg-sky-600 px-1 text-center text-[10px] leading-4 text-white tabular-nums"
                                >
                                    {count}
                                </motion.span>
                            )}
                        </AnimatePresence>
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent role="dialog" aria-label="Filters" minWidth={340} className="w-[340px] p-3.5">
                    <FilterPanel value={value} onChange={onChange} facets={facets} />
                </PopoverMenuContent>
            </PopoverMenu>
        </div>
    );
}
