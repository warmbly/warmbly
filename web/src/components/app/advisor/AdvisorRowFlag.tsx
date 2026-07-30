// The advisor indicator that lives on the row the problem is about.
//
// A list of twenty mailboxes with a stack of advice cards above it makes the
// reader do the join themselves: read "erik.lund is ramping too fast", scan the
// table, find the row. The flag removes that step. The advice is on the row, in
// the row's own tone, and opens in place.
//
// It renders nothing when the row is clean, which is most rows most of the
// time. That is the point: the column reads as empty until something is wrong,
// so a single amber dot in a healthy table is genuinely eye-catching.

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowRightIcon, CheckCircle2Icon, SparklesIcon } from "lucide-react";
import type { AdvisorFinding } from "@/lib/api/models/app/advisor/Advisor";
import {
    SEVERITY_DOT,
    SEVERITY_ROW,
    SEVERITY_SHORT,
    worstSeverity,
} from "@/lib/api/models/app/advisor/Advisor";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import AdvisorFixDrawer from "./AdvisorFixDrawer";

interface Props {
    findings: AdvisorFinding[];
    // subject names the row in the panel header ("liam.walsh@sunrise.test").
    subject?: string;
    className?: string;
}

export default function AdvisorRowFlag({ findings, subject, className = "" }: Props) {
    const [open, setOpen] = useState(false);
    const [reviewing, setReviewing] = useState<AdvisorFinding | null>(null);

    if (findings.length === 0) return null;

    const severity = worstSeverity(findings) ?? "low";
    const outstanding = findings.filter((f) => f.status !== "applied");
    // Everything on this row has been applied and is waiting for the next
    // evaluation to confirm it. Say so quietly rather than keeping the warning
    // tone on a row that has already been dealt with.
    const settled = outstanding.length === 0;

    return (
        <>
            <PopoverMenu align="end" open={open} onOpenChange={setOpen}>
                <PopoverMenuTrigger asChild>
                    <motion.button
                        type="button"
                        initial={{ opacity: 0, scale: 0.8 }}
                        animate={{ opacity: 1, scale: 1 }}
                        transition={{ type: "spring", stiffness: 500, damping: 30 }}
                        onClick={(e) => e.stopPropagation()}
                        aria-label={`${findings.length} suggestion${findings.length > 1 ? "s" : ""} for this row`}
                        className={`inline-flex h-5 shrink-0 items-center gap-1 rounded-full px-1.5 text-[10px] font-medium uppercase tracking-[0.08em] transition ${
                            settled ? "bg-emerald-50 text-emerald-700 hover:bg-emerald-100" : SEVERITY_ROW[severity]
                        } ${className}`}
                    >
                        {settled ? (
                            <CheckCircle2Icon className="h-2.5 w-2.5" />
                        ) : (
                            <span className="relative flex h-1.5 w-1.5">
                                {severity === "critical" ? (
                                    <span
                                        className={`absolute inline-flex h-full w-full animate-ping rounded-full opacity-60 ${SEVERITY_DOT[severity]}`}
                                    />
                                ) : null}
                                <span
                                    className={`relative inline-flex h-1.5 w-1.5 rounded-full ${SEVERITY_DOT[severity]}`}
                                />
                            </span>
                        )}
                        <span className="hidden sm:inline">
                            {settled ? "Applied" : SEVERITY_SHORT[severity]}
                        </span>
                        {outstanding.length > 1 ? (
                            <span className="tabular-nums">{outstanding.length}</span>
                        ) : null}
                    </motion.button>
                </PopoverMenuTrigger>

                <PopoverMenuContent minWidth={320} className="max-w-[380px] py-0">
                    <div className="flex items-center gap-1.5 border-b border-slate-100 px-2.5 py-2">
                        <SparklesIcon className="h-3 w-3 shrink-0 text-slate-400" />
                        <span className="min-w-0 flex-1 truncate text-[11px] text-slate-500">
                            {subject || "This row"}
                        </span>
                        <span className="shrink-0 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                            {findings.length} {findings.length === 1 ? "note" : "notes"}
                        </span>
                    </div>

                    <ul className="max-h-[300px] divide-y divide-slate-100 overflow-y-auto">
                        {findings.map((f, i) => (
                            <motion.li
                                key={f.id}
                                initial={{ opacity: 0, x: -4 }}
                                animate={{ opacity: 1, x: 0 }}
                                // Staggered so the panel resolves top-down and the
                                // eye lands on the most urgent one first.
                                transition={{ delay: i * 0.03, duration: 0.15 }}
                                className="px-2.5 py-2"
                            >
                                <div className="flex items-start gap-2">
                                    <span
                                        className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${
                                            f.status === "applied" ? "bg-emerald-500" : SEVERITY_DOT[f.severity]
                                        }`}
                                        aria-hidden
                                    />
                                    <div className="min-w-0 flex-1">
                                        <p className="text-[12px] font-medium leading-snug text-slate-900">
                                            {f.title}
                                        </p>
                                        <p className="mt-0.5 line-clamp-2 text-[11.5px] leading-relaxed text-slate-500">
                                            {f.remedy}
                                        </p>
                                    </div>
                                </div>

                                <div className="mt-1.5 pl-3.5">
                                    {f.status === "applied" ? (
                                        <span className="inline-flex items-center gap-1 text-[11px] text-emerald-600">
                                            <CheckCircle2Icon className="h-3 w-3" />
                                            Applied, waiting on the next check
                                        </span>
                                    ) : (
                                        <button
                                            type="button"
                                            onClick={() => {
                                                setOpen(false);
                                                setReviewing(f);
                                            }}
                                            className="inline-flex h-6 items-center gap-1 rounded-md border border-slate-200 px-1.5 text-[11px] font-medium text-slate-700 transition hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                                        >
                                            {f.action ? "Review the fix" : "How to fix it"}
                                            <ArrowRightIcon className="h-2.5 w-2.5" />
                                        </button>
                                    )}
                                </div>
                            </motion.li>
                        ))}
                    </ul>
                </PopoverMenuContent>
            </PopoverMenu>

            <AnimatePresence>
                {reviewing ? (
                    <AdvisorFixDrawer finding={reviewing} onClose={() => setReviewing(null)} />
                ) : null}
            </AnimatePresence>
        </>
    );
}
