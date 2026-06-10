// Form field primitives — slim, brae-density.
//
// Replaces the half-dozen ad-hoc inputs across pages. Two main pieces:
//
//   <SearchInput value={q} onChange={setQ} placeholder="Search…" />
//   <TextInput value={x} onChange={setX} placeholder="Domain" />
//
// All 28px tall, hairline border, 12.5px text, 12px horizontal padding,
// focus ring tuned to sky-200 so it blends with the rest of the chrome.

import React from "react";
import { SearchIcon, XIcon, ChevronUpIcon, ChevronDownIcon } from "lucide-react";
import { cn } from "@/lib/utils";

// 16px on mobile so iOS Safari doesn't auto-zoom on focus; 12.5px from md up.
const base =
    "h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[16px] md:text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100 disabled:bg-slate-50 disabled:text-slate-400";

export function TextInput({
    value,
    onChange,
    placeholder,
    type = "text",
    disabled,
    autoFocus,
    className,
    onKeyDown,
}: {
    value: string;
    onChange: (v: string) => void;
    placeholder?: string;
    type?: string;
    disabled?: boolean;
    autoFocus?: boolean;
    className?: string;
    onKeyDown?: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}) {
    return (
        <input
            type={type}
            value={value}
            placeholder={placeholder}
            disabled={disabled}
            autoFocus={autoFocus}
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={onKeyDown}
            className={cn(base, "min-w-0", className)}
        />
    );
}

export function SearchInput({
    value,
    onChange,
    placeholder = "Search…",
    autoFocus,
    className,
    onKeyDown,
    onSubmit,
}: {
    value: string;
    onChange: (v: string) => void;
    placeholder?: string;
    autoFocus?: boolean;
    className?: string;
    onKeyDown?: (e: React.KeyboardEvent<HTMLInputElement>) => void;
    onSubmit?: (v: string) => void;
}) {
    return (
        <div className={cn(
            "h-7 pl-2 pr-1 rounded-md border border-slate-200 bg-white flex items-center gap-1.5 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors min-w-0",
            className,
        )}>
            <SearchIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
            <input
                value={value}
                placeholder={placeholder}
                autoFocus={autoFocus}
                onChange={(e) => onChange(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === "Enter") onSubmit?.(value);
                    onKeyDown?.(e);
                }}
                className="flex-1 min-w-0 h-full bg-transparent outline-none text-[16px] md:text-[12.5px] text-slate-900 placeholder:text-slate-400"
            />
            {value && (
                <button
                    type="button"
                    onClick={() => onChange("")}
                    aria-label="Clear search"
                    className="size-5 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 flex items-center justify-center shrink-0 transition-colors"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

export function Label({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <label className={cn(
            "text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium block mb-1.5",
            className,
        )}>
            {children}
        </label>
    );
}

export function FieldRow({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <div className={cn("space-y-1.5", className)}>
            {children}
        </div>
    );
}

// NumberInput — our own number field. No native browser spinner (those
// stubby up/down arrows are stripped with appearance:none); instead a
// pair of themed chevron steppers sits flush on the right inside the
// field border. Value is a number; onChange always gets a clamped
// number. `suffix` renders a muted unit label (e.g. "emails / day")
// inside the field, before the steppers.
export function NumberInput({
    value,
    onChange,
    onCommit,
    min,
    max,
    step = 1,
    disabled,
    placeholder,
    suffix,
    align = "left",
    className,
}: {
    value: number;
    onChange: (value: number) => void;
    // Optional "commit point" distinct from the live onChange: fires on blur,
    // on Enter, and on each stepper click — but NOT on every keystroke. Use it
    // when the consumer wants to persist (e.g. a network save) only once the
    // user settles on a value, instead of mid-typing. Omitting it preserves the
    // original onChange-only behavior for every existing call site.
    onCommit?: (value: number) => void;
    min?: number;
    max?: number;
    step?: number;
    disabled?: boolean;
    placeholder?: string;
    suffix?: React.ReactNode;
    align?: "left" | "right" | "center";
    className?: string;
}) {
    const clamp = (n: number) => {
        if (Number.isNaN(n)) return min ?? 0;
        if (min !== undefined && n < min) return min;
        if (max !== undefined && n > max) return max;
        return n;
    };
    const commitValue = () => onCommit?.(clamp(Number.isFinite(value) ? value : min ?? 0));
    const bump = (dir: 1 | -1) => {
        if (disabled) return;
        const next = clamp((Number.isFinite(value) ? value : 0) + dir * step);
        onChange(next);
        onCommit?.(next);
    };
    const atMax = max !== undefined && value >= max;
    const atMin = min !== undefined && value <= min;
    return (
        <div
            className={cn(
                "relative inline-flex items-center h-7 rounded-md border border-slate-200 bg-white transition-colors focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100",
                disabled && "bg-slate-50",
                className,
            )}
        >
            <input
                type="number"
                inputMode="numeric"
                value={Number.isFinite(value) ? value : ""}
                disabled={disabled}
                placeholder={placeholder}
                onChange={(e) => {
                    const raw = e.target.value;
                    onChange(raw === "" ? min ?? 0 : clamp(Number(raw)));
                }}
                onBlur={onCommit ? commitValue : undefined}
                onKeyDown={
                    onCommit
                        ? (e) => {
                              if (e.key === "Enter") {
                                  e.preventDefault();
                                  commitValue();
                                  (e.target as HTMLInputElement).blur();
                              }
                          }
                        : undefined
                }
                className={cn(
                    "w-full min-w-0 h-full bg-transparent outline-none px-2.5 text-[16px] md:text-[12.5px] text-slate-900 tabular-nums disabled:text-slate-400",
                    "[appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none",
                    align === "right" && "text-right",
                    align === "center" && "text-center",
                )}
            />
            {suffix ? (
                <span className="pr-2 text-[11px] text-slate-400 whitespace-nowrap select-none">{suffix}</span>
            ) : null}
            <div className="flex flex-col self-stretch border-l border-slate-200 shrink-0">
                <button
                    type="button"
                    tabIndex={-1}
                    aria-label="Increase"
                    disabled={disabled || atMax}
                    onClick={() => bump(1)}
                    className="flex-1 px-1 flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-50 disabled:opacity-30 disabled:hover:bg-transparent transition-colors rounded-tr-md"
                >
                    <ChevronUpIcon className="w-3 h-3" />
                </button>
                <button
                    type="button"
                    tabIndex={-1}
                    aria-label="Decrease"
                    disabled={disabled || atMin}
                    onClick={() => bump(-1)}
                    className="flex-1 px-1 flex items-center justify-center border-t border-slate-200 text-slate-400 hover:text-slate-700 hover:bg-slate-50 disabled:opacity-30 disabled:hover:bg-transparent transition-colors rounded-br-md"
                >
                    <ChevronDownIcon className="w-3 h-3" />
                </button>
            </div>
        </div>
    );
}
