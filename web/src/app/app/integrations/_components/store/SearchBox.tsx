// The store's one search box. On a browse page it filters the results live
// through the address; anywhere else it suggests apps and categories as you
// type, arrow keys move, and Enter opens the highlighted one or every result.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseSearchSchema } from "@/lib/browse-other-lists";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowRightIcon, CornerDownLeftIcon } from "lucide-react";

import { SearchInput } from "@/components/ui/field";
import useClickOutside from "@/hooks/useClickOutside";
import { cn } from "@/lib/utils";

import { Highlight, ItemLogo, MakerLine } from "./AppCard";
import { CATEGORY_META, categoryLabel, searchItems, searchScore, STORE_CATEGORIES, type StoreCategory, type StoreItem } from "./model";

type Suggestion =
    | { kind: "item"; item: StoreItem }
    | { kind: "category"; category: StoreCategory; count: number }
    | { kind: "all"; count: number };

const MAX_APPS = 6;

export default function SearchBox({
    items,
    onOpenItem,
    onOpenCategory,
    onSearchAll,
    filter,
    browseKey,
    placeholder = "Search apps",
}: {
    items: StoreItem[];
    onOpenItem: (item: StoreItem) => void;
    onOpenCategory: (category: StoreCategory) => void;
    onSearchAll: (q: string) => void;
    /** On a browse page: the live query and its setter, instead of suggestions. */
    filter?: { value: string; onChange: (q: string) => void };
    browseKey: string;
    placeholder?: string;
}) {
    const [draft, setDraft] = useBrowseState(`integrations.store.${browseKey}.search`, "", browseSearchSchema);
    const value = filter ? filter.value : draft;
    const setValue = filter ? filter.onChange : setDraft;
    const [open, setOpen] = React.useState(false);
    const [active, setActive] = React.useState(0);
    const wrapRef = React.useRef<HTMLDivElement | null>(null);

    useClickOutside(open, () => setOpen(false), wrapRef);

    const q = value.trim();
    const suggestions = React.useMemo<Suggestion[]>(() => {
        if (!q || filter) return [];
        const matches = searchItems(items, q);
        const cats = STORE_CATEGORIES.map((c) => ({
            category: c,
            count: items.filter((it) => it.category === c).length,
            hit: categoryLabel(c).toLowerCase().includes(q.toLowerCase()) || items.some((it) => it.category === c && searchScore(it, q) >= 5),
        })).filter((c) => c.count > 0 && c.hit);
        return [
            ...matches.slice(0, MAX_APPS).map((item) => ({ kind: "item" as const, item })),
            ...cats.slice(0, 2).map((c) => ({ kind: "category" as const, category: c.category, count: c.count })),
            ...(matches.length ? [{ kind: "all" as const, count: matches.length }] : []),
        ];
    }, [items, q, filter]);

    React.useEffect(() => setActive(0), [q]);

    function choose(s: Suggestion) {
        setOpen(false);
        setValue("");
        if (s.kind === "item") onOpenItem(s.item);
        else if (s.kind === "category") onOpenCategory(s.category);
        else onSearchAll(q);
    }

    function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
        if (filter) return;
        if (!suggestions.length) {
            if (e.key === "Enter" && q) onSearchAll(q);
            return;
        }
        if (e.key === "ArrowDown") {
            e.preventDefault();
            setOpen(true);
            setActive((i) => (i + 1) % suggestions.length);
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((i) => (i - 1 + suggestions.length) % suggestions.length);
        } else if (e.key === "Enter") {
            e.preventDefault();
            choose(suggestions[active] ?? { kind: "all", count: 0 });
        }
    }

    const show = open && q.length > 0 && !filter;

    return (
        <div ref={wrapRef} className="relative w-full" onFocus={() => setOpen(true)}>
            <SearchInput
                value={value}
                onChange={(v) => {
                    setValue(v);
                    setOpen(true);
                }}
                onKeyDown={onKeyDown}
                placeholder={placeholder}
            />
            <AnimatePresence>
                {show && (
                    <motion.div
                        data-floating
                        role="listbox"
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute left-0 top-full mt-1 w-[min(400px,calc(100vw-2rem))] rounded-lg border border-slate-200 bg-white shadow-lg overflow-hidden z-30"
                    >
                        {suggestions.length === 0 && (
                            <div className="px-4 py-5 text-center text-[12.5px] text-slate-500">
                                No apps match “{q}”. Press Enter to search anyway.
                            </div>
                        )}
                        <div className="py-1.5 max-h-[60vh] overflow-y-auto">
                            {suggestions.map((s, i) => {
                                const on = i === active;
                                const base = cn(
                                    "w-full flex items-center gap-3 px-3 text-left transition-colors",
                                    on ? "bg-slate-50" : "hover:bg-slate-50",
                                );
                                if (s.kind === "item") {
                                    return (
                                        <button
                                            key={s.item.key}
                                            type="button"
                                            role="option"
                                            aria-selected={on}
                                            onMouseEnter={() => setActive(i)}
                                            onClick={() => choose(s)}
                                            className={cn(base, "py-2")}
                                        >
                                            <ItemLogo item={s.item} size={9} />
                                            <span className="min-w-0 flex-1">
                                                <span className="block text-[13px] font-medium text-slate-900 truncate">
                                                    <Highlight text={s.item.name} q={q} />
                                                </span>
                                                <span className="block text-[11.5px] text-slate-500 truncate">
                                                    <Highlight text={s.item.tagline} q={q} />
                                                </span>
                                            </span>
                                            <MakerLine item={s.item} className="hidden sm:inline-flex max-w-[110px]" />
                                        </button>
                                    );
                                }
                                if (s.kind === "category") {
                                    const meta = CATEGORY_META[s.category];
                                    return (
                                        <button
                                            key={`cat-${s.category}`}
                                            type="button"
                                            role="option"
                                            aria-selected={on}
                                            onMouseEnter={() => setActive(i)}
                                            onClick={() => choose(s)}
                                            className={cn(base, "py-2 border-t border-slate-100")}
                                        >
                                            <span className="w-9 h-9 rounded-md bg-slate-100 text-slate-500 inline-flex items-center justify-center shrink-0">
                                                <meta.icon className="w-4 h-4" />
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="block text-[13px] font-medium text-slate-900">{categoryLabel(s.category)}</span>
                                                <span className="block text-[11.5px] text-slate-500">Category · {s.count} apps</span>
                                            </span>
                                        </button>
                                    );
                                }
                                return (
                                    <button
                                        key="all"
                                        type="button"
                                        role="option"
                                        aria-selected={on}
                                        onMouseEnter={() => setActive(i)}
                                        onClick={() => choose(s)}
                                        className={cn(base, "h-10 border-t border-slate-100 text-[12.5px] text-sky-700 font-medium")}
                                    >
                                        <ArrowRightIcon className="w-4 h-4" />
                                        <span className="flex-1">
                                            See all {s.count} {s.count === 1 ? "result" : "results"} for “{q}”
                                        </span>
                                        {on && <CornerDownLeftIcon className="w-3.5 h-3.5 text-slate-400" />}
                                    </button>
                                );
                            })}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
