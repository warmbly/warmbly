// Small controls the checkout shares: a segmented choice with a sliding pill, a count stepper and a sender's avatar.
import React from "react";
import { motion, useReducedMotion } from "framer-motion";
import { MinusIcon, PlusIcon } from "lucide-react";
import RollingNumber from "@/components/ui/RollingNumber";
import { useStorePhotoUrl } from "@/lib/api/hooks/app/store/useStore";
import { PRESS_SPRING } from "@/lib/buy/motion";
import { cn } from "@/lib/utils";

export interface SegmentOption<T extends string> {
    value: T;
    label: React.ReactNode;
    /** The accessible name when the label is not plain text. */
    name?: string;
}

/** A radio group drawn as a segmented control; arrow keys move the choice like any radio group. */
export function Segmented<T extends string>({
    options,
    value,
    onChange,
    label,
    pillId,
    title,
    className,
}: {
    options: SegmentOption<T>[];
    value: T;
    onChange: (v: T) => void;
    label: string;
    /** Unique per control on the page, so its pill slides only within it. */
    pillId: string;
    title?: string;
    className?: string;
}) {
    const reduced = useReducedMotion();
    const refs = React.useRef<(HTMLButtonElement | null)[]>([]);
    const move = (from: number, by: number) => {
        const next = (from + by + options.length) % options.length;
        onChange(options[next].value);
        refs.current[next]?.focus();
    };
    return (
        <div role="radiogroup" aria-label={label} title={title} className={cn("relative inline-flex items-center h-8 p-0.5 rounded-lg bg-slate-100/80 border border-slate-200/70", className)}>
            {options.map((o, i) => {
                const on = o.value === value;
                return (
                    <button
                        key={o.value}
                        ref={(el) => {
                            refs.current[i] = el;
                        }}
                        type="button"
                        role="radio"
                        aria-checked={on}
                        aria-label={o.name}
                        tabIndex={on ? 0 : -1}
                        onClick={() => onChange(o.value)}
                        onKeyDown={(e) => {
                            if (e.key === "ArrowRight" || e.key === "ArrowDown") {
                                e.preventDefault();
                                move(i, 1);
                            } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
                                e.preventDefault();
                                move(i, -1);
                            }
                        }}
                        className={cn(
                            "relative flex-1 h-full px-2.5 rounded-md inline-flex items-center justify-center gap-1.5 text-[12px] font-medium whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                            on ? "text-slate-900" : "text-slate-500 hover:text-slate-800",
                        )}
                    >
                        {on && (
                            <motion.span
                                layoutId={reduced ? undefined : pillId}
                                transition={PRESS_SPRING}
                                className="absolute inset-0 rounded-md bg-white shadow-[0_1px_2px_rgba(15,23,42,0.08),0_0_0_1px_rgba(15,23,42,0.04)]"
                            />
                        )}
                        <span className="relative inline-flex items-center gap-1.5">{o.label}</span>
                    </button>
                );
            })}
        </div>
    );
}

/** A themed min..max stepper; the number rolls as it changes. */
export function CountStepper({ value, min, max, onChange, label, unit }: { value: number; min: number; max: number; onChange: (n: number) => void; label: string; unit: string }) {
    const reduced = useReducedMotion();
    const btn =
        "size-7 inline-flex items-center justify-center text-slate-600 hover:text-slate-900 hover:bg-slate-100 disabled:opacity-35 disabled:hover:bg-transparent transition-colors outline-none focus-visible:bg-slate-100";
    return (
        <div
            role="group"
            aria-label={label}
            className="inline-flex items-center h-8 rounded-lg border border-slate-200 bg-white overflow-hidden"
            onKeyDown={(e) => {
                if (e.key === "ArrowUp" || e.key === "ArrowRight") {
                    e.preventDefault();
                    if (value < max) onChange(value + 1);
                } else if (e.key === "ArrowDown" || e.key === "ArrowLeft") {
                    e.preventDefault();
                    if (value > min) onChange(value - 1);
                }
            }}
        >
            <motion.button type="button" aria-label={`One fewer ${unit}`} disabled={value <= min} onClick={() => onChange(value - 1)} whileTap={reduced ? undefined : { scale: 0.85 }} className={btn}>
                <MinusIcon className="w-3.5 h-3.5" />
            </motion.button>
            <output aria-live="polite" className="w-8 h-full border-x border-slate-200 inline-flex items-center justify-center text-[13px] font-semibold text-slate-900 tabular-nums">
                <RollingNumber value={value} />
            </output>
            <motion.button type="button" aria-label={`One more ${unit}`} disabled={value >= max} onClick={() => onChange(value + 1)} whileTap={reduced ? undefined : { scale: 0.85 }} className={btn}>
                <PlusIcon className="w-3.5 h-3.5" />
            </motion.button>
        </div>
    );
}

const TONES = ["bg-sky-100 text-sky-700", "bg-violet-100 text-violet-700", "bg-emerald-100 text-emerald-700", "bg-amber-100 text-amber-800", "bg-rose-100 text-rose-700"];

function initials(first: string, last: string): string {
    return `${first.trim()[0] ?? ""}${last.trim()[0] ?? ""}`.toUpperCase();
}

/** A sender's photo, or their initials on a colour of their own. `fallbackUrl` previews the account photo before it is copied. */
export function SenderAvatar({
    photoId,
    fallbackUrl,
    first,
    last,
    tone,
    className,
}: {
    photoId: string | null | undefined;
    fallbackUrl?: string | null;
    first: string;
    last: string;
    tone: number;
    className?: string;
}) {
    const url = useStorePhotoUrl(photoId) ?? fallbackUrl ?? null;
    return (
        <span className={cn("relative shrink-0 rounded-full overflow-hidden inline-flex items-center justify-center font-semibold", !url && TONES[tone % TONES.length], className)}>
            {url ? <img src={url} alt="" className="size-full object-cover" /> : <span className="leading-none">{initials(first, last) || "?"}</span>}
        </span>
    );
}
