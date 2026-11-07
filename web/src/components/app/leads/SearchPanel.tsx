import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    CircleIcon,
    DatabaseIcon,
    Loader2Icon,
    LockIcon,
    PentagonIcon,
    SearchIcon,
    SquareIcon,
    XIcon,
} from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuLabel, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { GeocodeResult, LatLng, LeadArea, LeadCategory, LeadFinderConfig, LeadProvider } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";
import LocationSearch from "./LocationSearch";
import SourceLogo from "./SourceLogo";
import { describeArea } from "./geo";
import type { MapTool } from "./LeadMap";
import { fmtUsdCost } from "@/lib/money";

// Shown as one-tap chips; the rest live in the full list.
const QUICK = ["restaurant", "dentist", "gym", "hair_salon", "plumber", "real_estate", "lawyer", "cafe", "car_repair", "hotel"];

export default function SearchPanel({
    config,
    query,
    onQuery,
    category,
    onCategory,
    location,
    onLocation,
    onPlace,
    near,
    provider,
    onProvider,
    area,
    onClearArea,
    onTool,
    searching,
    onSearch,
    blockedReason,
}: {
    config: LeadFinderConfig;
    query: string;
    onQuery: (v: string) => void;
    category: string;
    onCategory: (id: string) => void;
    location: string;
    onLocation: (v: string) => void;
    onPlace: (r: GeocodeResult) => void;
    near: LatLng | null;
    provider: LeadProvider;
    onProvider: (p: LeadProvider) => void;
    area: LeadArea | null;
    onClearArea: () => void;
    onTool: (t: MapTool) => void;
    searching: boolean;
    onSearch: () => void;
    blockedReason: string | null;
}) {
    const current = config.providers.find((p) => p.id === provider);
    const picked = config.categories.find((c) => c.id === category);
    const quick = QUICK.map((id) => config.categories.find((c) => c.id === id)).filter(Boolean) as LeadCategory[];

    return (
        <div className="space-y-3.5">
            <div>
                <div className="mb-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">What</div>
                <div className="relative">
                    <SearchIcon className="absolute left-2 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
                    <input
                        value={query}
                        onChange={(e) => onQuery(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && !blockedReason && !searching && onSearch()}
                        maxLength={120}
                        placeholder={picked ? `${picked.label}, optionally named…` : "Business type or name, e.g. vegan bakery"}
                        aria-label="What to look for"
                        className="h-8 w-full rounded-md border border-slate-200 bg-white pl-7 pr-2 text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                </div>
                <div className="mt-2 flex flex-wrap gap-1">
                    <AnimatePresence initial={false} mode="popLayout">
                        {picked && !QUICK.includes(picked.id) && (
                            <Chip key={picked.id} on onClick={() => onCategory("")}>
                                {picked.label}
                                <XIcon className="size-3" />
                            </Chip>
                        )}
                    </AnimatePresence>
                    {quick.map((c) => (
                        <Chip key={c.id} on={category === c.id} onClick={() => onCategory(category === c.id ? "" : c.id)}>
                            {c.label}
                        </Chip>
                    ))}
                    <CategoryMenu categories={config.categories} value={category} onChange={onCategory} />
                </div>
            </div>

            <div>
                <div className="mb-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Where</div>
                <LocationSearch value={location} onChange={onLocation} near={near} onPick={onPlace} />
                <div className="mt-2 flex items-center gap-1.5">
                    <span className="relative min-w-0 flex-1 overflow-hidden text-[11.5px]">
                        <AnimatePresence mode="wait" initial={false}>
                            <motion.span
                                key={area ? describeArea(area) : "view"}
                                initial={{ opacity: 0, y: 4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: -4 }}
                                transition={{ duration: 0.14 }}
                                className={cn("block truncate", area ? "font-medium text-sky-700" : "text-slate-500")}
                            >
                                {area ? describeArea(area) : "The visible map, or draw an area"}
                            </motion.span>
                        </AnimatePresence>
                    </span>
                    <ToolButton label="Draw a rectangle" onClick={() => onTool("rect")}>
                        <SquareIcon className="size-3.5" />
                    </ToolButton>
                    <ToolButton label="Draw a circle" onClick={() => onTool("circle")}>
                        <CircleIcon className="size-3.5" />
                    </ToolButton>
                    <ToolButton label="Draw a shape" onClick={() => onTool("polygon")}>
                        <PentagonIcon className="size-3.5" />
                    </ToolButton>
                    <AnimatePresence initial={false}>
                        {area && (
                            <motion.span key="clear" initial={{ opacity: 0, scale: 0.6 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.6 }} transition={{ duration: 0.14 }}>
                                <ToolButton label="Use the visible map instead" onClick={onClearArea}>
                                    <XIcon className="size-3.5" />
                                </ToolButton>
                            </motion.span>
                        )}
                    </AnimatePresence>
                </div>
            </div>

            <div>
                <div className="mb-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Source</div>
                <PopoverMenu>
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            className="h-8 w-full rounded-md border border-slate-200 bg-white px-2 text-left inline-flex items-center gap-2 text-[12.5px] text-slate-800 hover:border-slate-300"
                        >
                            {current ? <SourceLogo provider={current.id} name={current.name} /> : <DatabaseIcon className="size-3.5 text-slate-400" />}
                            <span className="truncate">{current?.name ?? "Pick a source"}</span>
                            {current && current.key_source !== "none" && (
                                <span className="rounded bg-slate-100 px-1 text-[10px] text-slate-500">{current.key_source === "workspace" ? "your key" : "included"}</span>
                            )}
                            <ChevronDownIcon className="ml-auto size-3.5 text-slate-400" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={300} matchTriggerWidth>
                        <PopoverMenuLabel>Where businesses come from</PopoverMenuLabel>
                        {config.providers.map((p) => (
                            <PopoverMenuItem
                                key={p.id}
                                disabled={!p.available}
                                selected={p.id === provider}
                                onSelect={() => onProvider(p.id)}
                                icon={<SourceLogo provider={p.id} name={p.name} size={7} />}
                                trailing={p.id === provider ? <CheckIcon className="size-3.5 text-sky-600" /> : !p.available ? <LockIcon className="size-3 text-slate-400" /> : undefined}
                                description={
                                    !p.available
                                        ? "Connect a key under Integrations"
                                        : p.cost_usd > 0
                                          ? `${fmtUsdCost(p.cost_usd)} a page of ${p.page_size}`
                                          : p.key_source === "workspace"
                                            ? "Billed to your own account"
                                            : "Free, no key needed"
                                }
                            >
                                {p.name}
                            </PopoverMenuItem>
                        ))}
                        <PopoverMenuSeparator />
                        <div className="px-2.5 py-1.5">
                            <Link to="/app/integrations/$" params={{ _splat: "category/data" }} className="text-[11.5px] text-sky-700 hover:underline">
                                Connect Google Places, Foursquare or Outscraper
                            </Link>
                        </div>
                    </PopoverMenuContent>
                </PopoverMenu>
                <AnimatePresence mode="wait" initial={false}>
                    {current && (
                        <motion.p
                            key={current.id}
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.14 }}
                            className="mt-1.5 text-[11px] leading-relaxed text-slate-400"
                        >
                            {current.description}
                        </motion.p>
                    )}
                </AnimatePresence>
            </div>

            <div>
                <motion.button
                    type="button"
                    disabled={searching || !!blockedReason}
                    onClick={onSearch}
                    whileTap={searching || blockedReason ? undefined : { scale: 0.98 }}
                    className="relative h-8 w-full overflow-hidden rounded-md bg-sky-600 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-1.5 hover:bg-sky-700 disabled:opacity-50 disabled:hover:bg-sky-600 transition-colors"
                >
                    {searching && <span className="absolute inset-y-0 w-1/3 bg-white/20 progress-sweep" aria-hidden />}
                    <AnimatePresence mode="wait" initial={false}>
                        <motion.span
                            key={searching ? "busy" : area ? "area" : "view"}
                            initial={{ opacity: 0, y: 5 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -5 }}
                            transition={{ duration: 0.14 }}
                            className="relative inline-flex items-center gap-1.5"
                        >
                            {searching ? <Loader2Icon className="size-3.5 animate-spin" /> : <SearchIcon className="size-3.5" />}
                            {searching ? "Searching…" : area ? "Search this area" : "Search the visible map"}
                        </motion.span>
                    </AnimatePresence>
                </motion.button>
                <AnimatePresence initial={false}>
                    {blockedReason && (
                        <motion.p
                            initial={{ opacity: 0, height: 0 }}
                            animate={{ opacity: 1, height: "auto" }}
                            exit={{ opacity: 0, height: 0 }}
                            transition={{ duration: 0.18 }}
                            className="overflow-hidden pt-1.5 text-[11px] text-amber-700"
                        >
                            {blockedReason}
                        </motion.p>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
}

// Takes a ref so popLayout can pull an exiting chip out of the flow at once.
function Chip({ on, onClick, children, ref }: { on?: boolean; onClick: () => void; children: React.ReactNode; ref?: React.Ref<HTMLButtonElement> }) {
    return (
        <motion.button
            ref={ref}
            layout
            type="button"
            onClick={onClick}
            aria-pressed={!!on}
            initial={{ opacity: 0, scale: 0.85 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.85 }}
            whileTap={{ scale: 0.94 }}
            transition={{ type: "spring", stiffness: 520, damping: 34, mass: 0.6 }}
            className={cn(
                "h-6 px-2 rounded-full border text-[11.5px] inline-flex items-center gap-1 transition-colors",
                on ? "border-sky-300 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
            )}
        >
            {children}
        </motion.button>
    );
}

function ToolButton({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            title={label}
            aria-label={label}
            onClick={onClick}
            className="size-6 rounded-md border border-slate-200 bg-white text-slate-500 hover:text-slate-900 hover:border-slate-300 inline-flex items-center justify-center"
        >
            {children}
        </button>
    );
}

function CategoryMenu({ categories, value, onChange }: { categories: LeadCategory[]; value: string; onChange: (id: string) => void }) {
    const [q, setQ] = useState("");
    const groups = useMemo(() => {
        const needle = q.trim().toLowerCase();
        const out = new Map<string, LeadCategory[]>();
        for (const c of categories) {
            if (needle && !c.label.toLowerCase().includes(needle) && !c.group.toLowerCase().includes(needle)) continue;
            out.set(c.group, [...(out.get(c.group) ?? []), c]);
        }
        return [...out.entries()];
    }, [categories, q]);
    return (
        <PopoverMenu onOpenChange={(o) => !o && setQ("")}>
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-6 px-2 rounded-full border border-dashed border-slate-300 text-[11.5px] text-slate-500 hover:text-slate-900 hover:border-slate-400 inline-flex items-center gap-1"
                >
                    All {categories.length} categories
                    <ChevronDownIcon className="size-3" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={280} role="dialog" aria-label="Categories">
                <div className="p-1.5 border-b border-slate-100">
                    <input
                        autoFocus
                        value={q}
                        onChange={(e) => setQ(e.target.value)}
                        placeholder="Search categories…"
                        className="h-7 w-full rounded border border-slate-200 px-2 text-[12px] outline-none focus:border-sky-400"
                    />
                </div>
                <div className="max-h-[320px] overflow-y-auto py-1">
                    {groups.length === 0 && <div className="px-3 py-4 text-center text-[11.5px] text-slate-400">No category matches</div>}
                    {groups.map(([group, list]) => (
                        <div key={group}>
                            <PopoverMenuLabel>{group}</PopoverMenuLabel>
                            {list.map((c) => (
                                <PopoverMenuItem
                                    key={c.id}
                                    onSelect={() => onChange(value === c.id ? "" : c.id)}
                                    trailing={value === c.id ? <CheckIcon className="size-3.5 text-sky-600" /> : undefined}
                                >
                                    {c.label}
                                </PopoverMenuItem>
                            ))}
                        </div>
                    ))}
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
