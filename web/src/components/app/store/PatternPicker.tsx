// Address patterns: a chip box of the chosen patterns, each shown as the first sender's real address, opening a
// list of every pattern to pick and order. The order is the order each sender's mailboxes take them in.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ArrowDownIcon, ArrowUpIcon, ChevronDownIcon, RotateCcwIcon } from "lucide-react";
import { CheckSquare } from "@/components/ui/check-square";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { ADDRESS_PATTERNS, PATTERN_NAMES, patternAddress, type AddressPattern, type NamedPerson } from "@/lib/buy/patterns";
import { cn } from "@/lib/utils";

/** The address a pattern makes from the sample, or the pattern itself when the sample cannot make it. */
function patternExample(sample: NamedPerson, p: AddressPattern): string {
    return patternAddress(sample, p) || p;
}

export default function PatternPicker({
    value,
    automatic,
    sample,
    onChange,
    onAuto,
    suggested,
}: {
    value: AddressPattern[];
    /** True while the patterns follow the number of senders. */
    automatic: boolean;
    sample: NamedPerson;
    onChange: (next: AddressPattern[]) => void;
    onAuto: () => void;
    suggested: AddressPattern[];
}) {
    const reduced = useReducedMotion();
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    const placement = useFlipPlacement(triggerRef, open, 360);

    const toggle = (p: AddressPattern) => {
        if (value.includes(p)) {
            // At least one pattern stays: the last one cannot be unticked.
            if (value.length > 1) onChange(value.filter((x) => x !== p));
        } else onChange([...value, p]);
    };
    const move = (p: AddressPattern, by: -1 | 1) => {
        const at = value.indexOf(p);
        const to = at + by;
        if (at < 0 || to < 0 || to >= value.length) return;
        const next = [...value];
        [next[at], next[to]] = [next[to], next[at]];
        onChange(next);
    };
    // Chosen first, in their order, then the rest in catalog order.
    const listed = [...value, ...ADDRESS_PATTERNS.filter((p) => !value.includes(p))];

    return (
        <div ref={ref} className="relative min-w-0">
            <button
                ref={triggerRef}
                type="button"
                onClick={() => setOpen((o) => !o)}
                aria-expanded={open}
                aria-haspopup="dialog"
                aria-label={`Address patterns: ${value.map((p) => `${patternExample(sample, p)}@`).join(", ")}. Change`}
                className={cn(
                    "w-full min-h-8 pl-1 pr-1.5 py-0.5 rounded-lg border bg-white flex items-center gap-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                    open ? "border-sky-400 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300",
                )}
            >
                <span className="flex-1 min-w-0 flex flex-wrap gap-1">
                    <AnimatePresence initial={false} mode="popLayout">
                        {value.map((p, i) => (
                            <motion.span
                                key={p}
                                layout={reduced ? false : "position"}
                                initial={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.85 }}
                                animate={{ opacity: 1, scale: 1 }}
                                exit={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.85, transition: { duration: 0.1 } }}
                                className="min-h-6 pl-1 pr-2 rounded-md bg-slate-100/80 border border-slate-200/70 inline-flex items-center gap-1 max-w-full"
                            >
                                <span className="size-4 rounded bg-white text-[9.5px] font-semibold text-slate-500 tabular-nums inline-flex items-center justify-center shrink-0">{i + 1}</span>
                                <span className="text-[12px] font-medium text-slate-800 [overflow-wrap:anywhere]">{patternExample(sample, p)}@</span>
                            </motion.span>
                        ))}
                    </AnimatePresence>
                </span>
                <ChevronDownIcon className={cn("w-3.5 h-3.5 text-slate-400 shrink-0 transition-transform", open && "rotate-180")} />
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        data-floating
                        role="dialog"
                        aria-label="Address patterns"
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={cn(
                            "absolute left-0 z-30 w-[min(380px,calc(100vw-32px))] rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden",
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                        )}
                    >
                        <p className="px-2.5 py-2 border-b border-slate-200 text-[11px] text-slate-500 leading-snug">
                            Tick the patterns to use. Each sender&apos;s mailboxes take them in this order; Alt and the arrow keys reorder.
                        </p>
                        <div role="list" className="max-h-72 overflow-y-auto py-1">
                            {listed.map((p) => {
                                const at = value.indexOf(p);
                                const on = at >= 0;
                                return (
                                    <motion.div
                                        key={p}
                                        role="listitem"
                                        layout={reduced ? false : "position"}
                                        transition={{ duration: 0.16 }}
                                        className="group px-1 flex items-center"
                                    >
                                        <button
                                            type="button"
                                            role="checkbox"
                                            aria-checked={on}
                                            aria-label={`${PATTERN_NAMES[p]}, like ${patternExample(sample, p)}@${on ? `, used ${ordinal(at + 1)}` : ""}`}
                                            onClick={() => toggle(p)}
                                            onKeyDown={(e) => {
                                                if (!e.altKey || !on || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
                                                e.preventDefault();
                                                move(p, e.key === "ArrowUp" ? -1 : 1);
                                            }}
                                            className="flex-1 min-w-0 min-h-8 py-1 px-1.5 rounded flex items-center gap-2 text-left outline-none hover:bg-slate-100 focus-visible:bg-slate-100 transition-colors"
                                        >
                                            <CheckSquare checked={on} tone="slate" />
                                            {/* Nothing here truncates: the address is what the person is choosing, so a long one wraps. */}
                                            <span className="min-w-0 flex-1 flex flex-col">
                                                <span className="text-[12.5px] font-medium text-slate-800 [overflow-wrap:anywhere]">{patternExample(sample, p)}@</span>
                                                <span className="text-[11px] text-slate-400 leading-snug">{on ? `${ordinal(at + 1)} · ${PATTERN_NAMES[p]}` : PATTERN_NAMES[p]}</span>
                                            </span>
                                        </button>
                                        {on && value.length > 1 && (
                                            <span className="flex items-center shrink-0">
                                                <ArrowButton label={`Move ${patternExample(sample, p)}@ earlier`} disabled={at === 0} onClick={() => move(p, -1)}>
                                                    <ArrowUpIcon className="w-3 h-3" />
                                                </ArrowButton>
                                                <ArrowButton label={`Move ${patternExample(sample, p)}@ later`} disabled={at === value.length - 1} onClick={() => move(p, 1)}>
                                                    <ArrowDownIcon className="w-3 h-3" />
                                                </ArrowButton>
                                            </span>
                                        )}
                                    </motion.div>
                                );
                            })}
                        </div>
                        <div className="px-2.5 py-1.5 border-t border-slate-200 flex items-center gap-2 text-[11px]">
                            {automatic ? (
                                <span className="text-emerald-700">Suggested for these senders</span>
                            ) : (
                                <button
                                    type="button"
                                    onClick={onAuto}
                                    className="h-6 -ml-1 px-1 rounded inline-flex items-center gap-1 font-medium text-sky-700 hover:text-sky-900 outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                                >
                                    <RotateCcwIcon className="w-3 h-3" /> Use the suggested {suggested.map((p) => `${patternExample(sample, p)}@`).join(", ")}
                                </button>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function ArrowButton({ label, disabled, onClick, children }: { label: string; disabled: boolean; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            aria-label={label}
            disabled={disabled}
            onClick={onClick}
            className="size-6 rounded text-slate-400 hover:text-slate-800 hover:bg-slate-100 disabled:opacity-30 disabled:hover:bg-transparent inline-flex items-center justify-center outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors"
        >
            {children}
        </button>
    );
}

function ordinal(n: number): string {
    return ["first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth", "ninth", "tenth", "eleventh", "twelfth"][n - 1] ?? `${n}th`;
}
