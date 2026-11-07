// A dollar field with themed steppers. Typing is free until the field is left
// (or Enter), then the amount is parsed and clamped, so mutually bounded fields
// never fight mid-keystroke the way a per-keystroke clamp does.

import React from "react";
import { ChevronDownIcon, ChevronUpIcon } from "lucide-react";
import { parseUsd, roundCents } from "@/lib/money";
import { cn } from "@/lib/utils";

const show = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2));

export default function UsdStepper({
    value,
    onCommit,
    min,
    max,
    step = 1,
    disabled,
    ariaLabel,
    className,
}: {
    value: number;
    onCommit: (n: number) => void;
    min: number;
    max: number;
    step?: number;
    disabled?: boolean;
    ariaLabel: string;
    className?: string;
}) {
    const [text, setText] = React.useState(show(value));
    const [focused, setFocused] = React.useState(false);
    React.useEffect(() => {
        if (!focused) setText(show(value));
    }, [value, focused]);

    const clamp = (n: number) => roundCents(Math.min(max, Math.max(min, n)));
    const commit = (n: number | null) => {
        const next = n === null ? value : clamp(n);
        setText(show(next));
        if (next !== value) onCommit(next);
    };
    // A step lands on the step grid, so $5.50 + $5 goes to $10, not $10.50.
    const bump = (dir: 1 | -1) => {
        if (disabled) return;
        const base = dir === 1 ? Math.floor(value / step) * step + step : Math.ceil(value / step) * step - step;
        commit(base);
    };

    return (
        <div
            className={cn(
                "relative inline-flex items-center h-7 rounded-md border border-slate-200 bg-white transition-colors focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100",
                disabled && "bg-slate-50",
                className,
            )}
        >
            <span className="pl-2.5 text-[12.5px] text-slate-400 select-none">$</span>
            <input
                type="text"
                inputMode="decimal"
                aria-label={ariaLabel}
                value={text}
                disabled={disabled}
                onFocus={(e) => {
                    setFocused(true);
                    e.currentTarget.select();
                }}
                onChange={(e) => setText(e.target.value.replace(/[^\d.,$\s]/g, ""))}
                onBlur={() => {
                    setFocused(false);
                    commit(parseUsd(text));
                }}
                onKeyDown={(e) => {
                    if (e.key === "Enter") e.currentTarget.blur();
                    if (e.key === "ArrowUp") {
                        e.preventDefault();
                        bump(1);
                    }
                    if (e.key === "ArrowDown") {
                        e.preventDefault();
                        bump(-1);
                    }
                }}
                className="w-full min-w-0 h-full bg-transparent outline-none pl-1 pr-2 text-[16px] md:text-[12.5px] text-slate-900 tabular-nums disabled:text-slate-400"
            />
            <div className="flex flex-col self-stretch border-l border-slate-200 shrink-0">
                <button
                    type="button"
                    tabIndex={-1}
                    aria-label={`Raise ${ariaLabel.toLowerCase()}`}
                    disabled={disabled || value >= max}
                    onClick={() => bump(1)}
                    className="flex-1 px-1 flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-50 disabled:opacity-30 disabled:hover:bg-transparent transition-colors rounded-tr-md"
                >
                    <ChevronUpIcon className="w-3 h-3" />
                </button>
                <button
                    type="button"
                    tabIndex={-1}
                    aria-label={`Lower ${ariaLabel.toLowerCase()}`}
                    disabled={disabled || value <= min}
                    onClick={() => bump(-1)}
                    className="flex-1 px-1 flex items-center justify-center border-t border-slate-200 text-slate-400 hover:text-slate-700 hover:bg-slate-50 disabled:opacity-30 disabled:hover:bg-transparent transition-colors rounded-br-md"
                >
                    <ChevronDownIcon className="w-3 h-3" />
                </button>
            </div>
        </div>
    );
}
