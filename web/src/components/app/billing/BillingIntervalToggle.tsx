// Monthly / Annual switch shared by the billing page and the upgrade dialog.

import { motion, useReducedMotion } from "framer-motion";
import type { BillingInterval } from "@/lib/pricing";

export default function BillingIntervalToggle({
    interval,
    onChange,
    size = "sm",
}: {
    interval: BillingInterval;
    onChange: (i: BillingInterval) => void;
    /** "md" is the larger variant used in the upgrade dialog hero. */
    size?: "sm" | "md";
}) {
    const reduced = useReducedMotion();
    const md = size === "md";
    return (
        <div
            role="radiogroup"
            aria-label="Billing interval"
            className={`inline-flex items-center rounded-md border border-slate-200 bg-slate-50 p-0.5 ${md ? "text-[13px]" : "text-[12px]"}`}
        >
            {(["monthly", "annual"] as BillingInterval[]).map((opt) => {
                const active = interval === opt;
                return (
                    <button
                        key={opt}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        onClick={() => onChange(opt)}
                        className={`relative rounded inline-flex items-center gap-1.5 font-medium transition-colors ${
                            md ? "h-8 px-3.5" : "h-6 px-2.5"
                        } ${active ? "text-slate-900" : "text-slate-500 hover:text-slate-700"}`}
                    >
                        {active && (
                            <motion.span
                                layoutId={`billing-interval-${size}`}
                                transition={reduced ? { duration: 0 } : { type: "spring", stiffness: 500, damping: 40 }}
                                className="absolute inset-0 rounded bg-white shadow-sm"
                            />
                        )}
                        <span className="relative">{opt === "monthly" ? "Monthly" : "Annual"}</span>
                        {opt === "annual" && (
                            <span
                                className={`relative text-[10px] font-semibold ${
                                    active ? "text-emerald-600" : "text-emerald-500"
                                }`}
                            >
                                −20%
                            </span>
                        )}
                    </button>
                );
            })}
        </div>
    );
}
