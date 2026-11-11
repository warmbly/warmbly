// The accent colour control: ready-made colours, a picker for any other, a way back to the
// default, and the scale drawn from the choice.

import { CheckIcon } from "lucide-react";

import ColorPicker from "@/components/ui/color-picker";
import { STEPS, accentScale, oklchCSS, stockScale } from "@/lib/accentScale";
import { cn } from "@/lib/utils";

const DEFAULT_ACCENT = "#0284c7";

// A spread across the wheel; the first is the default, stored as no choice.
const PRESETS: { hex: string; name: string }[] = [
    { hex: "", name: "Default blue" },
    { hex: "#4f46e5", name: "Indigo" },
    { hex: "#7c3aed", name: "Violet" },
    { hex: "#db2777", name: "Pink" },
    { hex: "#dc2626", name: "Red" },
    { hex: "#ea580c", name: "Orange" },
    { hex: "#16a34a", name: "Green" },
    { hex: "#0f766e", name: "Teal" },
    { hex: "#0f172a", name: "Ink" },
];

export default function AccentField({
    value,
    onChange,
    disabled,
    showScale = false,
}: {
    /** #rrggbb, or empty for the default. */
    value: string;
    onChange: (hex: string) => void;
    disabled: boolean;
    /** Draws the eleven shades the dashboard takes from the colour. */
    showScale?: boolean;
}) {
    const scale = (accentScale(value) ?? stockScale()).light;
    const current = value.toLowerCase();
    const custom = !!current && !PRESETS.some((p) => p.hex === current);
    return (
        <div className={cn(disabled && "opacity-60")}>
            <div className="flex flex-wrap items-center gap-1.5">
                {PRESETS.map((p) => {
                    const active = p.hex === current;
                    return (
                        <button
                            key={p.name}
                            type="button"
                            disabled={disabled}
                            onClick={() => onChange(p.hex)}
                            aria-label={p.name}
                            aria-pressed={active}
                            title={p.name}
                            className={cn(
                                "inline-flex size-7 items-center justify-center rounded-full outline-none ring-offset-2 transition-shadow focus-visible:ring-2 focus-visible:ring-sky-300",
                                active && "ring-2 ring-slate-900/70",
                            )}
                            style={{ background: p.hex || DEFAULT_ACCENT }}
                        >
                            {active && <CheckIcon className="size-3.5 text-white" strokeWidth={3} />}
                        </button>
                    );
                })}
                <span className="mx-1 h-5 w-px bg-slate-200" aria-hidden />
                {/* Any other colour; ringed when it is the one in use. */}
                <span className={cn("inline-flex rounded-md ring-offset-2", custom && "ring-2 ring-slate-900/70")}>
                    <ColorPicker
                        value={value}
                        fallback={DEFAULT_ACCENT}
                        onChange={(hex) => onChange(hex.toLowerCase())}
                        disabled={disabled}
                        aria-label="Accent colour"
                    />
                </span>
            </div>
            {showScale && (
                <div aria-hidden className="mt-3 flex h-2.5 w-full overflow-hidden rounded-full ring-1 ring-slate-900/5">
                    {STEPS.map((step) => (
                        <span key={step} className="flex-1" style={{ background: oklchCSS(scale[step]) }} />
                    ))}
                </div>
            )}
        </div>
    );
}
