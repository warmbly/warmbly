// FilterMenu — compact single-select dropdown for panel headers (the From
// menu's tag filter, the browse panel's category filter and sort). A quiet
// bordered trigger showing the active choice, and a portaled list of dot
// rows so it never clips inside an overflow-hidden panel.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, ChevronDownIcon, type LucideIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import { cn } from "@/lib/utils";

export interface FilterMenuOption {
    id: string;
    label: string;
    color?: string;
}

interface FilterMenuProps {
    icon: LucideIcon;
    /** Trigger label when nothing is selected; also the "All" row label. */
    allLabel: string;
    options: FilterMenuOption[];
    value: string | null;
    onChange: (id: string | null) => void;
    /** When false there is no "All" row (sort menus always have a value). */
    allowAll?: boolean;
}

export default function FilterMenu({
    icon: Icon,
    allLabel,
    options,
    value,
    onChange,
    allowAll = true,
}: FilterMenuProps) {
    const [open, setOpen] = React.useState(false);
    const [anchor, setAnchor] = React.useState<{ top: number; right: number; up: boolean } | null>(
        null,
    );
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    const active = options.find((o) => o.id === value) ?? null;

    const toggle = () => {
        const el = ref.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        const vh = window.innerHeight;
        const up = vh - r.bottom < 260 && r.top > vh - r.bottom;
        setAnchor({
            top: up ? r.top - 4 : r.bottom + 4,
            right: Math.max(8, document.documentElement.clientWidth - r.right),
            up,
        });
        setOpen((o) => !o);
    };

    const pick = (id: string | null) => {
        onChange(id);
        setOpen(false);
    };

    const row = (id: string | null, label: string, color?: string) => {
        const selected = id === value;
        return (
            <button
                key={id ?? "__all"}
                type="button"
                onClick={() => pick(id)}
                className="w-full px-2.5 h-7 flex items-center gap-2 text-left hover:bg-slate-50 transition-colors"
            >
                {color && (
                    <span className="size-1.5 rounded-full shrink-0" style={{ backgroundColor: color }} />
                )}
                <span className="flex-1 min-w-0 truncate text-[11.5px] text-slate-800">{label}</span>
                {selected && <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />}
            </button>
        );
    };

    return (
        <div ref={ref} className="shrink-0">
            <button
                type="button"
                onClick={toggle}
                className={cn(
                    "h-6 pl-1.5 pr-1 inline-flex items-center gap-1 rounded-md border text-[10.5px] font-medium transition-colors",
                    active && allowAll
                        ? "border-sky-300 bg-sky-50 text-sky-700"
                        : "border-slate-200 text-slate-500 hover:text-slate-700 hover:border-slate-300",
                )}
            >
                <Icon className="w-3 h-3 shrink-0" />
                {active?.color && (
                    <span
                        className="size-1.5 rounded-full shrink-0"
                        style={{ backgroundColor: active.color }}
                    />
                )}
                <span className="max-w-[88px] truncate">{active ? active.label : allLabel}</span>
                <ChevronDownIcon className="w-2.5 h-2.5 opacity-60 shrink-0" />
            </button>
            {createPortal(
                <AnimatePresence>
                    {open && anchor && (
                        <motion.div
                            data-floating=""
                            initial={{ opacity: 0, y: anchor.up ? 4 : -4, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, y: anchor.up ? 4 : -4, scale: 0.98 }}
                            transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                            style={{
                                position: "fixed",
                                right: anchor.right,
                                zIndex: 130,
                                ...(anchor.up
                                    ? { bottom: window.innerHeight - anchor.top }
                                    : { top: anchor.top }),
                            }}
                            className="w-[180px] max-h-[240px] overflow-y-auto rounded-md border border-slate-200 bg-white shadow-xl py-1"
                        >
                            {allowAll && row(null, allLabel)}
                            {options.map((o) => row(o.id, o.label, o.color))}
                        </motion.div>
                    )}
                </AnimatePresence>,
                document.body,
            )}
        </div>
    );
}
