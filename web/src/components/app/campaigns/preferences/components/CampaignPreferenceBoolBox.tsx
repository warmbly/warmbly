// On-theme primitives shared across the campaign settings tabs.
//
// Replaces the legacy off-theme Switch / Title / SubTitle usage. Everything
// here is slate/sky, rounded-md, 12.5px base, h-7 controls — matching the
// rebuilt analytics + campaign-overview chrome.

import React from "react";
import { CheckIcon } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * SettingRow — a labelled setting line. Title + helper text on the left,
 * an arbitrary control (Toggle, Segmented, NumberInput…) on the right.
 *
 * Mobile-first: by default the control stacks UNDER the title on phones and
 * moves to the classic right-justified position from sm+ (good for compact
 * controls: Toggle, NumberInput, the 2-option Segmented).
 *
 * `stack` forces the control onto its own full-width line beneath the title at
 * EVERY width. Use it for wide, long-label controls (the 3-option OptionSelect
 * for Rotation / ESP matching) so the control owns the full content width and
 * never competes with the label for horizontal space.
 */
export function SettingRow({
    title,
    description,
    control,
    children,
    stack = false,
}: {
    title: string;
    description?: React.ReactNode;
    control?: React.ReactNode;
    children?: React.ReactNode;
    stack?: boolean;
}) {
    return (
        <div
            className={cn(
                "min-w-0 flex flex-col gap-2.5",
                !stack && "sm:flex-row sm:items-start sm:justify-between sm:gap-5",
            )}
        >
            <div className="min-w-0 flex-1">
                <p className="text-[12.5px] text-slate-900 font-medium">{title}</p>
                {description && (
                    <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">{description}</p>
                )}
                {children}
            </div>
            {control && <div className={cn("min-w-0", stack ? "w-full" : "sm:shrink-0")}>{control}</div>}
        </div>
    );
}

/**
 * Legacy default export kept so any stray importer still compiles. Plain
 * justified flex row.
 */
export default function CampaignPreferenceBoolBox({ children }: { children: React.ReactNode }) {
    return (
        <div className="flex justify-between gap-5 items-center min-w-0">
            {children}
        </div>
    );
}

/**
 * Toggle — our small on-theme switch. Sky-600 when on, slate when off.
 * 28px wide track, no library defaults.
 */
export function Toggle({
    value,
    onChange,
    disabled,
    id,
}: {
    value: boolean;
    onChange: (v: boolean) => void;
    disabled?: boolean;
    id?: string;
}) {
    return (
        <button
            type="button"
            id={id}
            role="switch"
            aria-checked={value}
            disabled={disabled}
            onClick={() => onChange(!value)}
            className={`relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100 disabled:opacity-50 ${
                value ? "bg-sky-600" : "bg-slate-200"
            }`}
        >
            <span
                className={`inline-block size-3.5 rounded-full bg-white shadow-sm transition-transform ${
                    value ? "translate-x-[15px]" : "translate-x-[2px]"
                }`}
            />
        </button>
    );
}

/**
 * Segmented — a small on-theme pill group. Active option = bg-sky-600 white;
 * the rest are muted slate. Generic over the option value.
 */
export function Segmented<T extends string>({
    value,
    onChange,
    options,
    className,
}: {
    value: T;
    onChange: (v: T) => void;
    options: { value: T; label: string }[];
    className?: string;
}) {
    return (
        <div
            className={`inline-flex items-center gap-0.5 rounded-md border border-slate-200 bg-white p-0.5 ${
                className ?? ""
            }`}
        >
            {options.map((o) => {
                const active = o.value === value;
                return (
                    <button
                        key={o.value}
                        type="button"
                        onClick={() => onChange(o.value)}
                        className={`h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors ${
                            active ? "bg-sky-600 text-white" : "text-slate-500 hover:text-slate-900"
                        }`}
                    >
                        {o.label}
                    </button>
                );
            })}
        </div>
    );
}

/**
 * OptionSelect — a themed, mutually-exclusive option group (a styled radio
 * group) for 3+ choices whose labels can be long ("Least-recently-used").
 *
 * Layout: a vertical stack of full-width selectable rows on mobile, so any
 * label length fits at 360px with ZERO horizontal overflow — the label lives in
 * a min-w-0 flex-1 cell and wraps if it ever needs to, while the check
 * indicator stays shrink-0 on the right. On sm+ it optionally becomes an even
 * multi-column grid (`cols`) for a compact, intentional desktop layout; pass
 * cols={1} (default) to keep a single column everywhere.
 *
 * Active row = sky tint + sky border + filled sky check. Idle = slate border on
 * white. Mirrors Segmented's value / onChange / options API for a near drop-in
 * swap; `hint` is optional one-line helper text per option. Prefer Segmented
 * for compact 2-option choices and OptionSelect for wide/long-label ones.
 */
export function OptionSelect<T extends string>({
    value,
    onChange,
    options,
    cols = 1,
    className,
    "aria-label": ariaLabel,
}: {
    value: T;
    onChange: (v: T) => void;
    options: { value: T; label: string; hint?: string }[];
    /** Columns from the sm breakpoint up. Mobile is always a single column. */
    cols?: 1 | 2 | 3;
    className?: string;
    "aria-label"?: string;
}) {
    // Static class strings so Tailwind's JIT keeps them.
    const smCols = cols === 3 ? "sm:grid-cols-3" : cols === 2 ? "sm:grid-cols-2" : "";
    return (
        <div role="radiogroup" aria-label={ariaLabel} className={cn("grid grid-cols-1 gap-1.5", smCols, className)}>
            {options.map((o) => {
                const active = o.value === value;
                return (
                    <button
                        key={o.value}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        onClick={() => onChange(o.value)}
                        className={cn(
                            "group flex w-full items-start gap-2.5 rounded-md border px-3 py-2 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                            active
                                ? "border-sky-300 bg-sky-50"
                                : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50",
                        )}
                    >
                        <span className="min-w-0 flex-1">
                            <span
                                className={cn(
                                    "block text-[12px] font-medium leading-snug",
                                    active ? "text-sky-700" : "text-slate-700 group-hover:text-slate-900",
                                )}
                            >
                                {o.label}
                            </span>
                            {o.hint && (
                                <span
                                    className={cn(
                                        "mt-0.5 block text-[11px] leading-snug",
                                        active ? "text-sky-600/80" : "text-slate-400",
                                    )}
                                >
                                    {o.hint}
                                </span>
                            )}
                        </span>
                        <span
                            className={cn(
                                "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border transition-colors",
                                active
                                    ? "border-sky-600 bg-sky-600 text-white"
                                    : "border-slate-300 bg-white text-transparent group-hover:border-slate-400",
                            )}
                            aria-hidden="true"
                        >
                            <CheckIcon className="size-2.5" strokeWidth={3} />
                        </span>
                    </button>
                );
            })}
        </div>
    );
}

/**
 * EmailListInput — chip input for cc/bcc recipients, on-theme. Type an
 * address and press Enter/Tab/comma to add; backspace on an empty field
 * removes the last chip.
 */
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function EmailListInput({
    values,
    onChange,
    placeholder = "name@example.com",
}: {
    values: string[];
    onChange: (values: string[]) => void;
    placeholder?: string;
}) {
    const [draft, setDraft] = React.useState("");

    // Split on whitespace/comma/semicolon so a pasted or typed list of addresses
    // becomes individual chips, de-duplicated against what's already there.
    const addTokens = (text: string) => {
        const tokens = text
            .split(/[\s,;]+/)
            .map((t) => t.trim())
            .filter(Boolean);
        if (tokens.length === 0) return;
        const next = [...values];
        for (const t of tokens) if (!next.includes(t)) next.push(t);
        onChange(next);
    };

    const commit = () => {
        if (!draft.trim()) return;
        addTokens(draft);
        setDraft("");
    };

    return (
        <div className="rounded-md border border-slate-200 bg-white min-h-[34px] px-2 py-1.5 flex flex-wrap items-center gap-1 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
            {values.map((v, i) => {
                const invalid = !EMAIL_RE.test(v);
                return (
                    <span
                        key={`${v}-${i}`}
                        title={invalid ? "This doesn't look like a valid email address" : undefined}
                        className={`inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium max-w-[calc(100%-4rem)] ${
                            invalid ? "bg-rose-50 text-rose-600" : "bg-sky-50 text-sky-700"
                        }`}
                    >
                        <span className="truncate">{v}</span>
                        <button
                            type="button"
                            onClick={() => onChange(values.filter((_, idx) => idx !== i))}
                            aria-label={`Remove ${v}`}
                            className="opacity-70 hover:opacity-100 shrink-0"
                        >
                            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
                                <path d="M18 6 6 18M6 6l12 12" />
                            </svg>
                        </button>
                    </span>
                );
            })}
            <input
                value={draft}
                inputMode="email"
                onChange={(e) => setDraft(e.target.value)}
                onPaste={(e) => {
                    const text = e.clipboardData.getData("text");
                    if (/[\s,;]/.test(text)) {
                        e.preventDefault();
                        addTokens(text);
                        setDraft("");
                    }
                }}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === "Tab" || e.key === "," || e.key === ";") {
                        if (draft.trim()) {
                            e.preventDefault();
                            commit();
                        }
                    } else if (e.key === "Backspace" && draft === "" && values.length > 0) {
                        e.preventDefault();
                        onChange(values.slice(0, -1));
                    }
                }}
                onBlur={commit}
                placeholder={values.length === 0 ? placeholder : ""}
                className="flex-1 min-w-[120px] h-5 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
            />
        </div>
    );
}
