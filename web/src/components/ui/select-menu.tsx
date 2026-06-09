// SelectMenu — the app's themed dropdown, replacing native <select> everywhere.
// Built on the shared PopoverMenu primitives so it matches every other picker
// (slate border, sky accent, rounded-md, h-7 density) and never shows the OS
// dropdown chrome. Supports optional option groups (rendered as section labels)
// while preserving declared order.
//
// (Distinct from the unused shadcn/Radix `select.tsx`, which has a different
// theme + compound API.)

import React from "react";
import { ChevronDownIcon } from "lucide-react";
import {
    PopoverMenu,
    PopoverMenuTrigger,
    PopoverMenuContent,
    PopoverMenuItem,
} from "@/components/ui/popover-menu";

export interface SelectOption {
    value: string;
    label: string;
    /** Optional section header. Consecutive options sharing a group are grouped. */
    group?: string;
    disabled?: boolean;
    /** Optional leading glyph (e.g. a tinted lucide icon), shown in the trigger
     *  and the option row. */
    icon?: React.ReactNode;
}

export function SelectMenu({
    value,
    onChange,
    options,
    placeholder = "Select…",
    className,
    minWidth = 220,
    align = "start",
    disabled,
    fullWidth = false,
    "aria-label": ariaLabel,
}: {
    value: string;
    onChange: (value: string) => void;
    options: SelectOption[];
    placeholder?: string;
    className?: string;
    minWidth?: number;
    align?: "start" | "end" | "center";
    disabled?: boolean;
    // When true the trigger stretches to its container and the dropdown matches
    // the trigger's measured width (no 220px floor).
    fullWidth?: boolean;
    "aria-label"?: string;
}) {
    const [open, setOpen] = React.useState(false);
    const current = options.find((o) => o.value === value);

    // Group consecutive options that share a `group`, preserving order.
    const groups: { group?: string; items: SelectOption[] }[] = [];
    for (const o of options) {
        const last = groups[groups.length - 1];
        if (last && last.group === o.group) last.items.push(o);
        else groups.push({ group: o.group, items: [o] });
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align={align}>
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    disabled={disabled}
                    aria-label={ariaLabel}
                    className={`h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 ${fullWidth ? "flex w-full" : "inline-flex"} items-center gap-1.5 transition-colors disabled:opacity-60 disabled:cursor-not-allowed ${className ?? ""}`}
                >
                    {current?.icon && <span className="inline-flex shrink-0 items-center">{current.icon}</span>}
                    <span className={`truncate flex-1 text-left ${current ? "" : "text-slate-400"}`}>
                        {current?.label ?? placeholder}
                    </span>
                    <ChevronDownIcon className="w-3 h-3 text-slate-400 shrink-0" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={fullWidth ? 0 : minWidth} matchTriggerWidth={fullWidth} className="max-h-72 overflow-y-auto">
                {groups.map((grp, gi) => (
                    <React.Fragment key={gi}>
                        {grp.group && (
                            <div className="px-2 pt-1.5 pb-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                {grp.group}
                            </div>
                        )}
                        {grp.items.map((o) => (
                            <PopoverMenuItem
                                key={o.value}
                                disabled={o.disabled}
                                selected={o.value === value}
                                onSelect={() => onChange(o.value)}
                            >
                                {o.icon ? (
                                    <span className="flex items-center gap-1.5">
                                        <span className="inline-flex shrink-0 items-center">{o.icon}</span>
                                        <span className="truncate">{o.label}</span>
                                    </span>
                                ) : (
                                    o.label
                                )}
                            </PopoverMenuItem>
                        ))}
                    </React.Fragment>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

export default SelectMenu;
