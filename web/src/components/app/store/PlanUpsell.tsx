// Shown instead of the checkout while the workspace has no plan: buying mailboxes needs one.
import { motion, useReducedMotion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, LayersIcon } from "lucide-react";
import { HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";
import { cn } from "@/lib/utils";

export default function PlanUpsell({ compact = false, className }: { compact?: boolean; className?: string }) {
    const reduced = useReducedMotion();
    return (
        <motion.section
            initial={reduced ? false : { opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
            className={cn("rounded-lg border border-slate-200 bg-white", compact ? "p-3.5" : "px-5 py-8 flex flex-col items-center text-center", className)}
            aria-label="A plan is needed"
        >
            <div className={cn("flex gap-3", compact ? "items-start" : "flex-col items-center")}>
                <span className="size-9 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                    <LayersIcon className="w-4 h-4" />
                </span>
                <div className="min-w-0">
                    <p className="text-[13.5px] font-semibold text-slate-900">Buying mailboxes needs a Warmbly plan</p>
                    <p className={cn("mt-1 text-[12px] text-slate-500 leading-relaxed", !compact && "max-w-[46ch]")}>
                        Mailboxes you buy here send your campaigns and warm up through Warmbly, which a plan covers. Pick one, then come back: your cart is
                        kept in this browser.
                    </p>
                </div>
            </div>
            <motion.span
                whileHover={reduced ? undefined : HOVER_LIFT}
                whileTap={reduced ? undefined : { scale: 0.97 }}
                transition={PRESS_SPRING}
                className={cn("inline-flex rounded-md", compact ? "mt-3" : "mt-4")}
            >
                <Link
                    to="/app/settings/billing/{-$tab}"
                    params={{ tab: "plans" }}
                    className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-semibold inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-300 focus-visible:ring-offset-2"
                >
                    Choose a plan <ArrowRightIcon className="w-3.5 h-3.5" />
                </Link>
            </motion.span>
        </motion.section>
    );
}
