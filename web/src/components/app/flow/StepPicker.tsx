// StepPicker is the menu an add button opens: a search box over every kind of
// step the builder offers, grouped, with a one-line description each, fully
// keyboard driven. An item with children opens a second page (the list of steps
// a "Go to" can jump to).

import React from "react";
import { createPortal } from "react-dom";
import { motion } from "framer-motion";
import { ChevronLeftIcon, ChevronRightIcon, SearchIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import { StepTile, type StepTone } from "./StepCard";
import { cn } from "@/lib/utils";

export interface PickerItem {
    key: string;
    label: string;
    group: string;
    icon: React.ReactNode;
    tone: StepTone;
    tile?: React.ReactNode;
    description?: string;
    keywords?: string;
    // Shown instead of picking, when the item cannot be used here.
    disabledReason?: string;
    children?: PickerItem[];
    childrenTitle?: string;
    childrenEmpty?: string;
}

const W = 320;
const MAX_H = 440;

function matches(item: PickerItem, q: string) {
    if (!q) return true;
    const hay = `${item.label} ${item.group} ${item.description ?? ""} ${item.keywords ?? ""}`.toLowerCase();
    return q
        .toLowerCase()
        .split(/\s+/)
        .filter(Boolean)
        .every((w) => hay.includes(w));
}

export default function StepPicker({
    anchor,
    items,
    title = "Add a step",
    clearance = 0,
    onPick,
    onClose,
}: {
    anchor: DOMRect;
    items: PickerItem[];
    title?: string;
    // Horizontal distance from the button to keep clear (half a card).
    clearance?: number;
    // The search the item was picked from, so an item can preselect what it was found by.
    onPick: (item: PickerItem, query: string) => void;
    onClose: () => void;
}) {
    const panelRef = React.useRef<HTMLDivElement>(null);
    const inputRef = React.useRef<HTMLInputElement>(null);
    const listRef = React.useRef<HTMLDivElement>(null);
    const [query, setQuery] = React.useState("");
    const [page, setPage] = React.useState<PickerItem | null>(null);
    const [active, setActive] = React.useState(0);
    useClickOutside(true, onClose, panelRef);

    const source = page ? (page.children ?? []) : items;
    const visible = source.filter((i) => matches(i, query));
    const pickable = visible.filter((i) => !i.disabledReason);

    React.useEffect(() => setActive(0), [query, page]);
    React.useEffect(() => inputRef.current?.focus(), [page]);
    React.useEffect(() => {
        const el = listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`);
        el?.scrollIntoView({ block: "nearest" });
    }, [active]);

    const choose = (item: PickerItem) => {
        if (item.disabledReason) return;
        if (item.children) {
            setPage(item);
            setQuery("");
            return;
        }
        onPick(item, query);
    };

    const onKey = (e: React.KeyboardEvent) => {
        if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((a) => Math.min(pickable.length - 1, a + 1));
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((a) => Math.max(0, a - 1));
        } else if (e.key === "Enter") {
            e.preventDefault();
            const item = pickable[active];
            if (item) choose(item);
        } else if (e.key === "Backspace" && !query && page) {
            e.preventDefault();
            setPage(null);
        }
    };

    // Beside the button so the flow around it stays visible; below or above it
    // when there is no room to the side. Always inside the window.
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    // Clear of the cards centred on the button, not just the button itself.
    const cx = anchor.left + anchor.width / 2;
    const gap = Math.max(anchor.width / 2, clearance) + 12;
    const side = cx + gap + W <= vw - 8 ? "right" : cx - gap - W >= 8 ? "left" : null;
    const below = side !== null || anchor.bottom + 8 + MAX_H <= vh - 8 || anchor.top < vh / 2;
    const style: React.CSSProperties = side
        ? {
              left: side === "right" ? cx + gap : cx - gap - W,
              top: Math.max(8, Math.min(anchor.top - 12, vh - MAX_H - 8)),
              maxHeight: Math.min(MAX_H, vh - 16),
          }
        : below
          ? { left: Math.max(8, Math.min(anchor.left + anchor.width / 2 - W / 2, vw - W - 8)), top: Math.min(anchor.bottom + 8, vh - 200), maxHeight: Math.max(200, vh - anchor.bottom - 16) }
          : { left: Math.max(8, Math.min(anchor.left + anchor.width / 2 - W / 2, vw - W - 8)), bottom: vh - anchor.top + 8, maxHeight: Math.max(200, anchor.top - 16) };
    const origin = side === "right" ? "left top" : side === "left" ? "right top" : below ? "top center" : "bottom center";

    let lastGroup = "";
    let pickIndex = -1;
    return createPortal(
        <motion.div
            ref={panelRef}
            data-floating
            role="dialog"
            aria-label={page ? (page.childrenTitle ?? page.label) : title}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.1, ease: "easeOut" }}
            style={{ ...style, width: W, transformOrigin: origin }}
            className="fixed z-50 flex flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl"
            onKeyDown={onKey}
            onMouseDown={(e) => e.stopPropagation()}
        >
            <div className="flex items-center gap-2 border-b border-slate-200 px-3">
                {page ? (
                    <button
                        type="button"
                        aria-label="Back"
                        onClick={() => setPage(null)}
                        className="-ml-1 inline-flex size-6 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                        <ChevronLeftIcon className="size-4" />
                    </button>
                ) : (
                    <SearchIcon className="size-3.5 shrink-0 text-slate-400" />
                )}
                <input
                    ref={inputRef}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder={page ? `Search ${(page.childrenTitle ?? page.label).toLowerCase()}…` : "Search steps…"}
                    className="h-10 min-w-0 flex-1 bg-transparent text-[12.5px] text-slate-900 outline-none placeholder:text-slate-400"
                />
            </div>
            {page && (
                <div className="px-3 pt-2 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">{page.childrenTitle ?? page.label}</div>
            )}
            <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto p-1">
                {visible.length === 0 ? (
                    <p className="px-3 py-6 text-center text-[12px] text-slate-400">
                        {page && !query ? (page.childrenEmpty ?? "Nothing to pick here.") : "No step matches that."}
                    </p>
                ) : (
                    visible.map((item) => {
                        const header = !page && item.group !== lastGroup ? item.group : null;
                        lastGroup = item.group;
                        const idx = item.disabledReason ? -1 : ++pickIndex;
                        const isActive = idx === active;
                        return (
                            <React.Fragment key={item.key}>
                                {header && (
                                    <div className="px-2 pb-1 pt-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400 first:pt-1">{header}</div>
                                )}
                                <button
                                    type="button"
                                    data-index={idx}
                                    disabled={!!item.disabledReason}
                                    title={item.disabledReason}
                                    onMouseEnter={() => idx >= 0 && setActive(idx)}
                                    onClick={() => choose(item)}
                                    className={cn(
                                        "flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors",
                                        item.disabledReason ? "cursor-not-allowed opacity-50" : isActive ? "bg-slate-100" : "hover:bg-slate-50",
                                    )}
                                >
                                    {item.tile ?? (
                                        <StepTile tone={item.tone} size="sm">
                                            {item.icon}
                                        </StepTile>
                                    )}
                                    <span className="min-w-0 flex-1">
                                        <span className="block truncate text-[12.5px] font-medium text-slate-800">{item.label}</span>
                                        {(item.disabledReason || item.description) && (
                                            <span className="block truncate text-[11px] text-slate-400">{item.disabledReason ?? item.description}</span>
                                        )}
                                    </span>
                                    {item.children && <ChevronRightIcon className="size-3.5 shrink-0 text-slate-300" />}
                                </button>
                            </React.Fragment>
                        );
                    })
                )}
            </div>
            <div className="flex items-center gap-3 border-t border-slate-200 px-3 py-1.5 text-[10.5px] text-slate-400">
                <span>
                    <kbd className="font-sans">↑↓</kbd> to move
                </span>
                <span>
                    <kbd className="font-sans">↵</kbd> to add
                </span>
                <span>
                    <kbd className="font-sans">esc</kbd> to close
                </span>
            </div>
        </motion.div>,
        document.body,
    );
}
