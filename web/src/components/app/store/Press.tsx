// Buttons for the buy flow: a small hover lift and a springy press, flat under reduced motion.
import React from "react";
import { motion, useReducedMotion, type HTMLMotionProps } from "framer-motion";
import { HOVER_LIFT, PRESS_SPRING, REST_SHADOW } from "@/lib/buy/motion";
import { cn } from "@/lib/utils";

type Tone = "primary" | "secondary" | "ghost" | "danger";

const TONES: Record<Tone, string> = {
    primary: "bg-sky-600 hover:bg-sky-700 text-white shadow-[0_1px_0_rgba(255,255,255,0.18)_inset,0_1px_2px_rgba(2,132,199,0.25)]",
    secondary: "bg-white border border-slate-200 hover:border-slate-300 text-slate-800",
    ghost: "text-slate-600 hover:text-slate-900 hover:bg-slate-100",
    danger: "bg-white border border-red-300 hover:border-red-400 text-red-700 hover:bg-red-50",
};

export type PressButtonProps = Omit<HTMLMotionProps<"button">, "children"> & {
    tone?: Tone;
    children?: React.ReactNode;
};

export function PressButton({ tone = "primary", className, disabled, type = "button", children, style, ...rest }: PressButtonProps) {
    const reduced = useReducedMotion();
    const live = !reduced && !disabled;
    return (
        <motion.button
            type={type}
            disabled={disabled}
            whileHover={live ? HOVER_LIFT : undefined}
            style={{ ...REST_SHADOW, ...style }}
            whileTap={live ? { scale: 0.97, y: 0 } : undefined}
            transition={PRESS_SPRING}
            className={cn(
                "relative h-7 px-2.5 rounded-md inline-flex items-center justify-center gap-1.5 text-[12px] font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200 focus-visible:ring-offset-1 disabled:opacity-55 disabled:cursor-not-allowed",
                TONES[tone],
                className,
            )}
            {...rest}
        >
            {children}
        </motion.button>
    );
}
