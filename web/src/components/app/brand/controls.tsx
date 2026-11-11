// The brand editor's own controls: a titled group, a field with its label on top, and a row of
// picture tiles for a choice that is easier to see than to read.

import React from "react";
import { CheckIcon } from "lucide-react";

import { cn } from "@/lib/utils";

/** One titled block of the editor's panel. */
export function Group({ title, description, children }: { title: string; description?: React.ReactNode; children: React.ReactNode }) {
    return (
        <section className="px-4 py-5 md:px-6">
            <h3 className="text-[13px] font-semibold tracking-tight text-slate-900">{title}</h3>
            {description && <p className="mt-0.5 text-[12px] leading-relaxed text-slate-500">{description}</p>}
            <div className="mt-4 space-y-4">{children}</div>
        </section>
    );
}

/** A label above a control that takes the panel's width, with its help underneath. */
export function Field({
    label,
    hint,
    aside,
    children,
}: {
    label: string;
    hint?: React.ReactNode;
    /** Sits at the end of the label's line: a counter, a reset. */
    aside?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div>
            <div className="mb-1.5 flex items-center gap-2">
                <span className="text-[12px] font-medium text-slate-700">{label}</span>
                {aside && <span className="ml-auto">{aside}</span>}
            </div>
            {children}
            {hint && <p className="mt-1.5 text-[11.5px] leading-snug text-slate-500">{hint}</p>}
        </div>
    );
}

export interface TileOption<T extends string> {
    value: T;
    label: string;
    /** The picture of the choice; drawn above the label. */
    glyph: React.ReactNode;
}

/** A single choice shown as pictures. Arrow keys move it, as in any radio group. */
export function Tiles<T extends string>({
    label,
    value,
    onChange,
    options,
    disabled = false,
    cols,
}: {
    /** Names the group for assistive technology. */
    label: string;
    value: T;
    onChange: (v: T) => void;
    options: TileOption<T>[];
    disabled?: boolean;
    cols: 2 | 3 | 4;
}) {
    const refs = React.useRef<(HTMLButtonElement | null)[]>([]);
    const move = (from: number, by: number) => {
        const next = (from + by + options.length) % options.length;
        onChange(options[next].value);
        refs.current[next]?.focus();
    };
    return (
        <div
            role="radiogroup"
            aria-label={label}
            className={cn("grid gap-2", cols === 2 ? "grid-cols-2" : cols === 3 ? "grid-cols-3" : "grid-cols-4", disabled && "opacity-60")}
        >
            {options.map((o, i) => {
                const active = o.value === value;
                return (
                    <button
                        key={o.value || "default"}
                        ref={(el) => {
                            refs.current[i] = el;
                        }}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        tabIndex={active ? 0 : -1}
                        disabled={disabled}
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
                            "group relative flex min-w-0 flex-col items-stretch gap-1.5 rounded-lg border p-1.5 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-sky-200",
                            active ? "border-sky-500 bg-sky-50/60" : "border-slate-200 bg-white hover:border-slate-300",
                        )}
                    >
                        <span className="flex h-12 items-center justify-center overflow-hidden rounded-md bg-slate-50 ring-1 ring-inset ring-slate-900/5">
                            {o.glyph}
                        </span>
                        <span className="flex items-center gap-1 px-0.5">
                            <span className={cn("truncate text-[11.5px]", active ? "font-medium text-sky-800" : "text-slate-600")}>{o.label}</span>
                            {active && <CheckIcon className="ml-auto size-3 shrink-0 text-sky-600" strokeWidth={3} />}
                        </span>
                    </button>
                );
            })}
        </div>
    );
}
