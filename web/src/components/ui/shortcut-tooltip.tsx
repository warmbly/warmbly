// ShortcutTooltip — the house pattern for "this has a fast key". Wraps any
// trigger with the themed tooltip and renders the keyboard combo as a kbd
// chip, translating "mod" to ⌘ on Apple platforms and Ctrl elsewhere. Use it
// anywhere a control has a shortcut so fast keys are discoverable across the
// dashboard instead of hiding in native title attributes.

import * as React from "react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./tooltip";

const IS_APPLE =
    typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.platform);

// shortcutLabel("mod+J") → "⌘J" on Apple, "Ctrl+J" elsewhere. Also maps
// "shift"/"alt"/"enter" to their symbols on Apple platforms.
export function shortcutLabel(combo: string): string {
    const parts = combo.split("+").map((p) => {
        const key = p.trim().toLowerCase();
        switch (key) {
            case "mod":
                return IS_APPLE ? "⌘" : "Ctrl";
            case "shift":
                return IS_APPLE ? "⇧" : "Shift";
            case "alt":
                return IS_APPLE ? "⌥" : "Alt";
            case "enter":
                return IS_APPLE ? "↵" : "Enter";
            default:
                return p.trim().toUpperCase();
        }
    });
    return IS_APPLE ? parts.join("") : parts.join("+");
}

export function Kbd({
    combo,
    variant = "dark",
}: {
    combo: string;
    // "dark" sits on the tooltip's dark surface; "light" is for inline hints
    // on white surfaces (popovers, pills).
    variant?: "dark" | "light";
}) {
    return (
        <kbd
            className={
                "inline-flex h-4 items-center rounded border px-1 font-mono text-[10px] leading-none " +
                (variant === "dark"
                    ? "border-white/25 bg-white/10"
                    : "border-slate-200 bg-slate-50 text-slate-500")
            }
        >
            {shortcutLabel(combo)}
        </kbd>
    );
}

export default function ShortcutTooltip({
    label,
    combo,
    side = "top",
    children,
}: {
    label: string;
    combo: string;
    side?: "top" | "bottom" | "left" | "right";
    children: React.ReactNode;
}) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>{children}</TooltipTrigger>
            <TooltipContent side={side} sideOffset={6} className="flex items-center gap-1.5">
                {label}
                <Kbd combo={combo} />
            </TooltipContent>
        </Tooltip>
    );
}
