// One recommendation. Collapsed it is a single readable line with its severity
// and its primary action; expanded it shows the reasoning, the evidence the
// detector fired on, and the ways to make it go away.
//
// The interaction model is deliberately quiet. A card never blocks the page,
// never animates on its own, and every destructive-feeling control (dismiss,
// snooze) is one click behind the overflow menu so the common path stays "read
// it, fix it, move on".

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckCircle2Icon,
    ChevronDownIcon,
    ClockIcon,
    MoreHorizontalIcon,
    ThumbsDownIcon,
    ThumbsUpIcon,
    Undo2Icon,
    XIcon,
} from "lucide-react";
import type { AdvisorFinding } from "@/lib/api/models/app/advisor/Advisor";
import {
    CATEGORY_LABEL,
    SEVERITY_CHIP,
    SEVERITY_DOT,
    SEVERITY_LABEL,
    evidenceLabel,
    evidenceValue,
} from "@/lib/api/models/app/advisor/Advisor";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import {
    useAdvisorFeedback,
    useDismissAdvisorFinding,
    useSnoozeAdvisorFinding,
    useUndoAdvisorFinding,
} from "@/lib/api/hooks/app/advisor/useAdvisor";

interface Props {
    finding: AdvisorFinding;
    onFix: (finding: AdvisorFinding) => void;
    // compact hides the category chip and evidence, for the inline strip on a
    // detail page where the context is already obvious.
    compact?: boolean;
    defaultOpen?: boolean;
}

// Evidence keys that only exist to identify the subject, which the card header
// already shows.
const REDUNDANT_EVIDENCE = new Set(["mailbox", "campaign", "step"]);

export default function AdvisorCard({ finding, onFix, compact = false, defaultOpen = false }: Props) {
    const [open, setOpen] = useState(defaultOpen);
    const [voted, setVoted] = useState<boolean | null>(null);
    const confirm = useConfirm();

    const snooze = useSnoozeAdvisorFinding();
    const dismiss = useDismissAdvisorFinding();
    const undo = useUndoAdvisorFinding();
    const feedback = useAdvisorFeedback();

    const applied = finding.status === "applied";
    const evidence = Object.entries(finding.evidence ?? {}).filter(
        ([key, value]) => !REDUNDANT_EVIDENCE.has(key) && value !== null && value !== "",
    );

    async function onDismiss() {
        confirm.show(
            "Stop suggesting this? It won't come back unless the problem clears and then happens again.",
            async () => {
                await dismiss.mutateAsync({ id: finding.id, reason: "" });
            },
        );
    }

    async function vote(helpful: boolean) {
        setVoted(helpful);
        await feedback.mutateAsync({ id: finding.id, helpful });
    }

    return (
        <div
            className={`group rounded-md border bg-white transition ${
                applied ? "border-emerald-200 bg-emerald-50/30" : "border-slate-200 hover:border-slate-300"
            }`}
        >
            <div className="flex items-start gap-2 px-2.5 py-2">
                <span
                    className={`mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full ${
                        applied ? "bg-emerald-500" : SEVERITY_DOT[finding.severity]
                    }`}
                    aria-hidden
                />

                <button
                    type="button"
                    onClick={() => setOpen((v) => !v)}
                    aria-expanded={open}
                    className="min-w-0 flex-1 text-left"
                >
                    <div className="flex items-center gap-1.5">
                        <span className="truncate text-[12.5px] font-medium text-slate-900">{finding.title}</span>
                        {applied ? (
                            <span className="inline-flex shrink-0 items-center gap-1 rounded-md border border-emerald-200 bg-emerald-50 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700">
                                <CheckCircle2Icon className="h-2.5 w-2.5" />
                                Applied
                            </span>
                        ) : null}
                    </div>
                    {!open ? (
                        <p className="mt-0.5 line-clamp-1 text-[11.5px] text-slate-500">{finding.detail}</p>
                    ) : null}
                </button>

                <div className="flex shrink-0 items-center gap-1">
                    {/* A finding with no one-click fix still opens the flow:
                        that is where its ordered how-to lives, and without this
                        the steps would be unreachable from a card. */}
                    {!applied ? (
                        <button
                            type="button"
                            onClick={(e) => {
                                e.stopPropagation();
                                onFix(finding);
                            }}
                            className={`inline-flex h-7 items-center rounded-md px-2 text-[12px] font-medium opacity-100 transition md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100 ${
                                finding.action
                                    ? "bg-sky-600 text-white hover:bg-sky-700"
                                    : "border border-slate-200 text-slate-600 hover:bg-slate-50"
                            }`}
                        >
                            {finding.action ? "Fix" : "How to fix"}
                        </button>
                    ) : null}

                    {applied && finding.action?.undo ? (
                        <button
                            type="button"
                            onClick={(e) => {
                                e.stopPropagation();
                                void undo.mutateAsync(finding.id);
                            }}
                            disabled={undo.isPending}
                            className="inline-flex h-7 items-center gap-1 rounded-md border border-slate-200 px-2 text-[12px] text-slate-600 transition hover:bg-slate-50 disabled:opacity-50"
                        >
                            <Undo2Icon className="h-3 w-3" />
                            Undo
                        </button>
                    ) : null}

                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label="More options"
                                onClick={(e) => e.stopPropagation()}
                                className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 transition hover:bg-slate-100 hover:text-slate-600"
                            >
                                <MoreHorizontalIcon className="h-3.5 w-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent>
                            <PopoverMenuLabel>Remind me later</PopoverMenuLabel>
                            <PopoverMenuItem
                                icon={<ClockIcon className="h-3.5 w-3.5" />}
                                onSelect={() => void snooze.mutateAsync({ id: finding.id, days: 7 })}
                            >
                                Snooze for a week
                            </PopoverMenuItem>
                            <PopoverMenuItem
                                icon={<ClockIcon className="h-3.5 w-3.5" />}
                                onSelect={() => void snooze.mutateAsync({ id: finding.id, days: 30 })}
                            >
                                Snooze for a month
                            </PopoverMenuItem>
                            <PopoverMenuItem
                                icon={<XIcon className="h-3.5 w-3.5" />}
                                onSelect={() => void onDismiss()}
                            >
                                Stop suggesting this
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>

                    <button
                        type="button"
                        onClick={() => setOpen((v) => !v)}
                        aria-label={open ? "Collapse" : "Expand"}
                        className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 transition hover:bg-slate-100 hover:text-slate-600"
                    >
                        <ChevronDownIcon
                            className={`h-3.5 w-3.5 transition-transform ${open ? "rotate-180" : ""}`}
                        />
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
                        <div className="space-y-2.5 border-t border-slate-100 px-2.5 py-2.5">
                            <p className="text-[12.5px] leading-relaxed text-slate-600">{finding.detail}</p>

                            <div className="rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-2">
                                <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                    What to do
                                </p>
                                <p className="mt-1 text-[12.5px] leading-relaxed text-slate-700">{finding.remedy}</p>
                            </div>

                            {!compact && evidence.length > 0 ? (
                                <div>
                                    <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                        What we measured
                                    </p>
                                    <dl className="mt-1.5 flex flex-wrap gap-1.5">
                                        {evidence.map(([key, value]) => (
                                            <div
                                                key={key}
                                                className="inline-flex items-baseline gap-1.5 rounded-md border border-slate-200 bg-white px-1.5 py-0.5"
                                            >
                                                <dt className="text-[11px] text-slate-500">{evidenceLabel(key)}</dt>
                                                <dd className="text-[11.5px] font-medium text-slate-800">
                                                    {evidenceValue(value)}
                                                </dd>
                                            </div>
                                        ))}
                                    </dl>
                                </div>
                            ) : null}

                            <div className="flex items-center justify-between gap-2 pt-0.5">
                                <div className="flex items-center gap-1.5">
                                    <span
                                        className={`inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] ${SEVERITY_CHIP[finding.severity]}`}
                                    >
                                        {SEVERITY_LABEL[finding.severity]}
                                    </span>
                                    {!compact ? (
                                        <span className="inline-flex items-center rounded-md border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.08em] text-slate-500">
                                            {CATEGORY_LABEL[finding.category]}
                                        </span>
                                    ) : null}
                                </div>

                                {/* Feedback is the loop that keeps the thresholds honest: a
                                    detector that everybody marks unhelpful is a detector that
                                    is wrong, not a user who is wrong. */}
                                <div className="flex items-center gap-0.5">
                                    <span className="mr-1 text-[11px] text-slate-400">Useful?</span>
                                    <button
                                        type="button"
                                        onClick={() => void vote(true)}
                                        aria-label="This was useful"
                                        aria-pressed={voted === true}
                                        className={`inline-flex h-6 w-6 items-center justify-center rounded-md transition ${
                                            voted === true
                                                ? "bg-emerald-50 text-emerald-600"
                                                : "text-slate-400 hover:bg-slate-100 hover:text-slate-600"
                                        }`}
                                    >
                                        <ThumbsUpIcon className="h-3 w-3" />
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => void vote(false)}
                                        aria-label="This was not useful"
                                        aria-pressed={voted === false}
                                        className={`inline-flex h-6 w-6 items-center justify-center rounded-md transition ${
                                            voted === false
                                                ? "bg-rose-50 text-rose-600"
                                                : "text-slate-400 hover:bg-slate-100 hover:text-slate-600"
                                        }`}
                                    >
                                        <ThumbsDownIcon className="h-3 w-3" />
                                    </button>
                                </div>
                            </div>
                        </div>
                    </motion.div>
                ) : null}
            </AnimatePresence>
        </div>
    );
}
