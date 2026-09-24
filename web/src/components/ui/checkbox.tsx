// The dashboard's checkbox. A real <input type="checkbox"> with the browser's
// drawing switched off (appearance-none), so labels, keyboard, focus, form
// semantics and click events behave natively while every browser renders the
// same themed square as CheckSquare. Never ship a bare native checkbox.
import * as React from "react";
import { CheckIcon } from "lucide-react";
import { cn } from "@/lib/utils";

const BOX = { xs: "size-3", sm: "size-3.5" } as const;
const MARK = { xs: "size-2", sm: "size-2.5" } as const;
const TONE = {
    sky: "checked:bg-sky-600 checked:border-sky-600",
    slate: "checked:bg-slate-900 checked:border-slate-900",
} as const;

export type CheckboxProps = Omit<React.InputHTMLAttributes<HTMLInputElement>, "type" | "size"> & {
    /** sky for row selection, slate for option toggles inside forms and dialogs. */
    tone?: keyof typeof TONE;
    size?: keyof typeof BOX;
    ref?: React.Ref<HTMLInputElement>;
};

/** className styles the wrapper (layout, margins, visibility); the box itself is fixed. */
export function Checkbox({ className, tone = "sky", size = "sm", ref, ...props }: CheckboxProps) {
    return (
        <span className={cn("relative inline-flex shrink-0 items-center justify-center", className)}>
            <input
                ref={ref}
                type="checkbox"
                {...props}
                className={cn(
                    "peer m-0 appearance-none cursor-pointer rounded-[4px] border border-slate-300 bg-white transition-colors",
                    "hover:border-slate-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-200",
                    "disabled:cursor-not-allowed disabled:opacity-50",
                    BOX[size],
                    TONE[tone],
                )}
            />
            <CheckIcon
                aria-hidden
                strokeWidth={3.5}
                className={cn("pointer-events-none absolute text-white opacity-0 peer-checked:opacity-100", MARK[size])}
            />
        </span>
    );
}

export default Checkbox;
