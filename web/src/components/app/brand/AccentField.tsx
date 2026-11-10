// The accent colour control: the picker, a way back to the default, and the scale drawn from it.

import { XIcon } from "lucide-react";

import ColorPicker from "@/components/ui/color-picker";
import { STEPS, accentScale, oklchCSS, stockScale } from "@/lib/accentScale";

const DEFAULT_ACCENT = "#0284c7";

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
    return (
        <div className="flex flex-col items-start gap-2 sm:items-end">
            <div className="flex items-center gap-1.5">
                <ColorPicker
                    value={value}
                    fallback={DEFAULT_ACCENT}
                    onChange={(hex) => onChange(hex.toLowerCase())}
                    disabled={disabled}
                    aria-label="Accent colour"
                />
                {value && (
                    <button
                        type="button"
                        disabled={disabled}
                        onClick={() => onChange("")}
                        aria-label="Use the default colour"
                        className="size-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 disabled:opacity-50"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                )}
            </div>
            {showScale && (
                <div aria-hidden className="flex h-3 w-[176px] overflow-hidden rounded-sm ring-1 ring-slate-900/5">
                    {STEPS.map((step) => (
                        <span key={step} className="flex-1" style={{ background: oklchCSS(scale[step]) }} />
                    ))}
                </div>
            )}
        </div>
    );
}
