// The one-line advisor summary that sits above a list page.
//
// Once every row carries its own flag, a stack of cards above the table is
// telling the reader something they can already see, in a place where it pushes
// the actual content down the page. So this collapses to a single line: how many
// things need attention, and a way in.
//
// It opens itself when there is something a row flag cannot express: a critical
// finding, or advice about the workspace rather than about any one row.

import { useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronDownIcon, SparklesIcon } from "lucide-react";
import type { AdvisorFinding, AdvisorSurface } from "@/lib/api/models/app/advisor/Advisor";
import {
    SEVERITY_DOT,
    groupFindings,
    indexByEntity,
    sortFindings,
} from "@/lib/api/models/app/advisor/Advisor";
import { SURFACE_FETCH_LIMIT, useAdvisorFindings } from "@/lib/api/hooks/app/advisor/useAdvisor";
import AdvisorCard from "./AdvisorCard";
import AdvisorFixDrawer from "./AdvisorFixDrawer";
import AdvisorGroupCard from "./AdvisorGroupCard";

interface Props {
    surface: AdvisorSurface;
    // What one row of this page is, for the count line.
    noun?: string;
    nounPlural?: string;
    className?: string;
}

const EASE = [0.16, 1, 0.3, 1] as const;

export default function AdvisorSummaryBar({
    surface,
    noun = "item",
    nounPlural = "items",
    className = "",
}: Props) {
    const { data } = useAdvisorFindings({ surface, limit: SURFACE_FETCH_LIMIT });
    const [fixing, setFixing] = useState<AdvisorFinding | null>(null);
    const [manuallyOpen, setManuallyOpen] = useState<boolean | null>(null);

    const view = useMemo(() => {
        const findings = sortFindings(data ?? []);
        const open = findings.filter((f) => f.status !== "applied");
        const index = indexByEntity(open);

        // How many distinct rows on this page are implicated. That is the
        // number a person scanning the table actually wants.
        const subjects = new Set<string>();
        for (const f of open) {
            const id = f.parent_id ?? f.entity_id;
            if (id) subjects.add(id);
        }

        return {
            findings,
            open,
            subjects: subjects.size,
            unattached: index.unattached,
            critical: open.filter((f) => f.severity === "critical").length,
            groups: groupFindings(findings),
        };
    }, [data]);

    if (view.findings.length === 0) return null;

    // Anything a row cannot show has to be visible without a click.
    const forceOpen = view.critical > 0 || view.unattached.length > 0;
    const expanded = manuallyOpen ?? forceOpen;

    // Tinted washes rather than fills, so the bar sits on the page instead of
    // covering a strip of it.
    const tone =
        view.critical > 0
            ? "border-rose-500/20 bg-rose-500/[0.06]"
            : view.open.length > 0
              ? "border-slate-200/80 bg-white/60"
              : "border-emerald-500/20 bg-emerald-500/[0.06]";

    return (
        <>
            <section className={`overflow-hidden rounded-md border backdrop-blur-sm ${tone} ${className}`}>
                <button
                    type="button"
                    onClick={() => setManuallyOpen(!expanded)}
                    aria-expanded={expanded}
                    className="flex w-full items-center gap-2 px-2.5 py-2 text-left transition hover:bg-slate-500/[0.04]"
                >
                    <SparklesIcon className="h-3.5 w-3.5 shrink-0 text-slate-400" />

                    <span className="min-w-0 flex-1 truncate text-[12.5px] text-slate-700">
                        <span className="font-medium text-slate-900">{summaryLine(view, noun, nounPlural)}</span>
                        {view.subjects > 0 ? (
                            <span className="hidden text-slate-500 sm:inline">
                                {" "}
                                · each one is flagged on its row
                            </span>
                        ) : null}
                    </span>

                    {/* A severity read at a glance, without a legend to learn. */}
                    <span className="hidden shrink-0 items-center gap-1 sm:flex">
                        {(["critical", "high", "medium", "low"] as const).map((s) => {
                            const n = view.open.filter((f) => f.severity === s).length;
                            if (n === 0) return null;
                            return (
                                <span
                                    key={s}
                                    className="inline-flex items-center gap-1 rounded-full bg-slate-500/[0.08] px-1.5 py-0.5 text-[10px] tabular-nums text-slate-600"
                                >
                                    <span className={`h-1.5 w-1.5 rounded-full ${SEVERITY_DOT[s]}`} />
                                    {n}
                                </span>
                            );
                        })}
                    </span>

                    <span className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-1.5 text-[11.5px] text-slate-500">
                        {expanded ? "Hide" : "Review"}
                        <ChevronDownIcon
                            className={`h-3 w-3 transition-transform ${expanded ? "rotate-180" : ""}`}
                        />
                    </span>
                </button>

                <AnimatePresence initial={false}>
                    {expanded ? (
                        <motion.div
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: "auto", opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={{ duration: 0.22, ease: EASE }}
                            className="overflow-hidden"
                        >
                            <div className="space-y-1.5 border-t border-slate-200/60 px-2.5 py-2.5">
                                {view.groups.map((group, i) => (
                                    <motion.div
                                        key={group.key}
                                        initial={{ opacity: 0, y: -4 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        transition={{ delay: i * 0.03, duration: 0.18, ease: EASE }}
                                    >
                                        {group.members.length > 1 ? (
                                            <AdvisorGroupCard group={group} onFix={setFixing} />
                                        ) : (
                                            <AdvisorCard finding={group.lead} onFix={setFixing} />
                                        )}
                                    </motion.div>
                                ))}
                            </div>
                        </motion.div>
                    ) : null}
                </AnimatePresence>
            </section>

            <AnimatePresence>
                {fixing ? <AdvisorFixDrawer finding={fixing} onClose={() => setFixing(null)} /> : null}
            </AnimatePresence>
        </>
    );
}

function summaryLine(
    view: { open: AdvisorFinding[]; subjects: number; critical: number },
    noun: string,
    nounPlural: string,
): string {
    if (view.open.length === 0) return "Everything suggested here has been applied";
    if (view.subjects === 0) {
        return `${view.open.length} ${view.open.length === 1 ? "suggestion" : "suggestions"} for this workspace`;
    }
    const subject = view.subjects === 1 ? noun : nounPlural;
    if (view.critical > 0) return `${view.subjects} ${subject} need urgent attention`;
    return `${view.subjects} ${subject} could be doing better`;
}
