// One check that fired on several things at once, as a single card.
//
// A workspace that raised the daily cap on twenty mailboxes has one problem
// with twenty subjects, not twenty problems. Showing it twenty times is how a
// surface becomes wallpaper, so the run collapses: one heading, one
// explanation, the affected list underneath, and one button that fixes all of
// them.

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronDownIcon, Loader2Icon } from "lucide-react";
import type { AdvisorFinding, AdvisorGroup } from "@/lib/api/models/app/advisor/Advisor";
import {
    CATEGORY_LABEL,
    SEVERITY_CHIP,
    SEVERITY_DOT,
    SEVERITY_LABEL,
    groupTitle,
} from "@/lib/api/models/app/advisor/Advisor";
import { useApplyAdvisorFinding } from "@/lib/api/hooks/app/advisor/useAdvisor";
import { useConfirm } from "@/hooks/context/confirm";

interface Props {
    group: AdvisorGroup;
    // onFix opens the single-finding preview drawer, used when a member is
    // fixed on its own.
    onFix: (finding: AdvisorFinding) => void;
}

export default function AdvisorGroupCard({ group, onFix }: Props) {
    const [open, setOpen] = useState(false);
    const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
    const apply = useApplyAdvisorFinding();
    const confirm = useConfirm();

    const { lead, members } = group;
    const fixable = members.filter((m) => m.action && m.status !== "applied");
    const title = groupTitle(group);

    // Fixing the whole run is the same confirm-first contract as one card: the
    // dialog names every mailbox it will touch and what changes on each.
    function fixAll() {
        if (fixable.length === 0) return;
        const preview = fixable
            .slice(0, 6)
            .map((m) => `• ${m.entity_label}: ${m.action?.label ?? "apply the fix"}`)
            .join("\n");
        const more = fixable.length > 6 ? `\n…and ${fixable.length - 6} more` : "";

        confirm.show(
            `Apply this fix to ${fixable.length} of them?\n\n${preview}${more}\n\nEach change runs as you and is written to the audit log.`,
            async () => {
                setProgress({ done: 0, total: fixable.length });
                try {
                    // Sequential rather than parallel: these are writes against
                    // the same workspace, and a burst of them would race the
                    // realtime invalidations against each other.
                    for (let i = 0; i < fixable.length; i++) {
                        await apply.mutateAsync(fixable[i].id);
                        setProgress({ done: i + 1, total: fixable.length });
                    }
                } finally {
                    setProgress(null);
                }
            },
        );
    }

    const busy = progress !== null;

    return (
        <div className="group rounded-md border border-slate-200/80 bg-white/70 backdrop-blur-sm transition hover:border-slate-300">
            <div className="flex items-start gap-2 px-2.5 py-2">
                <span
                    className={`mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full ${SEVERITY_DOT[lead.severity]}`}
                    aria-hidden
                />

                <button
                    type="button"
                    onClick={() => setOpen((v) => !v)}
                    aria-expanded={open}
                    className="min-w-0 flex-1 text-left"
                >
                    <div className="flex items-center gap-1.5">
                        <span className="truncate text-[12.5px] font-medium text-slate-900">{title}</span>
                        <span className="shrink-0 rounded border border-slate-200/70 bg-slate-500/[0.06] px-1 text-[10px] tabular-nums text-slate-500">
                            {members.length}
                        </span>
                    </div>
                    {!open ? (
                        <p className="mt-0.5 line-clamp-1 text-[11.5px] text-slate-500">{lead.remedy}</p>
                    ) : null}
                </button>

                <div className="flex shrink-0 items-center gap-1">
                    {fixable.length > 0 ? (
                        <button
                            type="button"
                            onClick={(e) => {
                                e.stopPropagation();
                                fixAll();
                            }}
                            disabled={busy}
                            className="inline-flex h-7 items-center gap-1.5 rounded-md bg-sky-600 px-2 text-[12px] font-medium text-white transition hover:bg-sky-700 disabled:opacity-60"
                        >
                            {busy ? <Loader2Icon className="h-3 w-3 animate-spin" /> : null}
                            {busy ? `${progress?.done}/${progress?.total}` : `Fix all ${fixable.length}`}
                        </button>
                    ) : null}

                    <button
                        type="button"
                        onClick={() => setOpen((v) => !v)}
                        aria-label={open ? "Collapse" : "Expand"}
                        className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 transition hover:bg-slate-100 hover:text-slate-600"
                    >
                        <ChevronDownIcon className={`h-3.5 w-3.5 transition-transform ${open ? "rotate-180" : ""}`} />
                    </button>
                </div>
            </div>

            <AnimatePresence initial={false}>
                {open ? (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.16, ease: "easeOut" }}
                        className="overflow-hidden"
                    >
                        <div className="space-y-2.5 border-t border-slate-200/60 px-2.5 py-2.5">
                            <p className="text-[12.5px] leading-relaxed text-slate-600">{lead.detail}</p>

                            <div className="rounded-md border border-slate-200/70 bg-white/50 px-2.5 py-2">
                                <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                    What to do
                                </p>
                                <p className="mt-1 text-[12.5px] leading-relaxed text-slate-700">{lead.remedy}</p>
                            </div>

                            <div>
                                <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                    Affected
                                </p>
                                <ul className="mt-1.5 divide-y divide-slate-200/50 rounded-md border border-slate-200/70">
                                    {members.map((m) => (
                                        <li key={m.id} className="flex items-center gap-2 px-2 py-1.5">
                                            <span className="min-w-0 flex-1 truncate text-[12px] text-slate-700">
                                                {m.entity_label || "This workspace"}
                                            </span>
                                            {m.status === "applied" ? (
                                                <span className="shrink-0 text-[11px] text-emerald-600">Applied</span>
                                            ) : (
                                                <button
                                                    type="button"
                                                    onClick={() => onFix(m)}
                                                    className="shrink-0 rounded-md border border-slate-200 px-1.5 py-0.5 text-[11px] text-slate-600 transition hover:bg-slate-50"
                                                >
                                                    {m.action ? "Fix" : "How"}
                                                </button>
                                            )}
                                        </li>
                                    ))}
                                </ul>
                            </div>

                            <div className="flex items-center gap-1.5 pt-0.5">
                                <span
                                    className={`inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] ${SEVERITY_CHIP[lead.severity]}`}
                                >
                                    {SEVERITY_LABEL[lead.severity]}
                                </span>
                                <span className="inline-flex items-center rounded-md border border-slate-200/70 bg-slate-500/[0.06] px-1.5 py-0.5 text-[10px] uppercase tracking-[0.08em] text-slate-500">
                                    {CATEGORY_LABEL[lead.category]}
                                </span>
                            </div>
                        </div>
                    </motion.div>
                ) : null}
            </AnimatePresence>
        </div>
    );
}
