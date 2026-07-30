// The resolution flow: why this fired, what will change, and what happened.
//
// A one-click fix that just applies is fast and untrustworthy. A wall of text
// with a button at the bottom is trustworthy and nobody reads it. This walks
// the middle: one short screen per question, in the order a person actually
// asks them, with the evidence in front of the decision rather than behind it.
//
// Every screen is reachable in both directions and nothing is applied until the
// screen that shows the exact before and after has been seen.

import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckIcon,
    Loader2Icon,
    ShieldCheckIcon,
    Undo2Icon,
    XIcon,
} from "lucide-react";
import type { AdvisorFinding } from "@/lib/api/models/app/advisor/Advisor";
import {
    SEVERITY_CHIP,
    SEVERITY_LABEL,
    evidenceLabel,
    evidenceValue,
    findingLink,
    resolutionSteps,
} from "@/lib/api/models/app/advisor/Advisor";
import {
    useApplyAdvisorFinding,
    useUndoAdvisorFinding,
} from "@/lib/api/hooks/app/advisor/useAdvisor";

interface Props {
    finding: AdvisorFinding | null;
    onClose: () => void;
}

// Evidence keys that only name the subject, which the header already shows.
const REDUNDANT_EVIDENCE = new Set(["mailbox", "campaign", "step"]);

type Stage = "why" | "change" | "done";

const EASE = [0.16, 1, 0.3, 1] as const;

export default function AdvisorFixDrawer({ finding, onClose }: Props) {
    const [stage, setStage] = useState<Stage>("why");
    const [applying, setApplying] = useState(false);
    const [failed, setFailed] = useState<string | null>(null);
    // Direction drives which way the screens slide, so going back reads as
    // going back rather than as another step forward.
    const direction = useRef(1);

    const apply = useApplyAdvisorFinding();
    const undo = useUndoAdvisorFinding();

    const action = finding?.action;
    const link = finding ? findingLink(finding) : null;
    const steps = useMemo(() => (finding ? resolutionSteps(finding) : []), [finding]);

    const evidence = useMemo(
        () =>
            Object.entries(finding?.evidence ?? {}).filter(
                ([key, value]) => !REDUNDANT_EVIDENCE.has(key) && value !== null && value !== "",
            ),
        [finding],
    );

    // A fix that was already applied opens on its outcome, not on a pitch to
    // apply it again.
    useEffect(() => {
        if (!finding) return;
        setStage(finding.status === "applied" ? "done" : "why");
        setFailed(null);
        direction.current = 1;
    }, [finding]);

    // Escape closes, but never mid-apply: dismissing the dialog does not cancel
    // the request, and a drawer that vanishes while the change is landing
    // leaves the user unsure whether it happened.
    useEffect(() => {
        if (!finding) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !applying) onClose();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [finding, applying, onClose]);

    if (!finding) return null;

    const go = (next: Stage, dir: number) => {
        direction.current = dir;
        setStage(next);
    };

    async function runFix() {
        if (!finding || applying) return;
        setApplying(true);
        setFailed(null);
        try {
            await apply.mutateAsync(finding.id);
            direction.current = 1;
            setStage("done");
        } catch {
            // Stay on the change screen with the reason visible. Closing the
            // drawer on failure would leave the row flagged with no explanation.
            setFailed("That change did not go through. Nothing was modified.");
        } finally {
            setApplying(false);
        }
    }

    // The rail only claims the steps this finding actually has. A finding with
    // no one-click fix never shows a third dot it can't reach.
    const rail: { id: Stage; label: string }[] = action
        ? [
              { id: "why", label: "Why" },
              { id: "change", label: "What changes" },
              { id: "done", label: "Done" },
          ]
        : [
              { id: "why", label: "Why" },
              { id: "change", label: "How to fix it" },
          ];
    const railIndex = Math.max(0, rail.findIndex((s) => s.id === stage));

    return (
        <motion.div
            className="fixed inset-0 z-50 flex items-end justify-center sm:items-center"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.12 }}
        >
            <div
                className="absolute inset-0 bg-slate-900/20 backdrop-blur-[1px]"
                onMouseDown={() => {
                    if (!applying) onClose();
                }}
            />

            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label={finding.title}
                className="relative w-full max-w-lg overflow-hidden rounded-t-lg border border-slate-200 bg-white shadow-[0_16px_48px_-12px_rgba(15,23,42,0.25)] sm:rounded-md"
                initial={{ y: 16, opacity: 0, scale: 0.99 }}
                animate={{ y: 0, opacity: 1, scale: 1 }}
                exit={{ y: 8, opacity: 0, scale: 0.99 }}
                transition={{ type: "spring", stiffness: 420, damping: 32 }}
            >
                <div className="flex shrink-0 items-start gap-2 px-3 pt-2.5">
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                            <span
                                className={`inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] ${SEVERITY_CHIP[finding.severity]}`}
                            >
                                {SEVERITY_LABEL[finding.severity]}
                            </span>
                            {finding.entity_label ? (
                                <span className="truncate text-[11px] text-slate-500">
                                    {finding.entity_label}
                                </span>
                            ) : null}
                        </div>
                        <h2 className="mt-1 text-[13px] font-medium leading-snug text-slate-900">
                            {finding.title}
                        </h2>
                    </div>
                    <button
                        type="button"
                        onClick={onClose}
                        disabled={applying}
                        aria-label="Close"
                        className="mt-0.5 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-400 transition hover:bg-slate-100 hover:text-slate-600 disabled:opacity-40"
                    >
                        <XIcon className="h-3.5 w-3.5" />
                    </button>
                </div>

                {/* Progress rail. Segments already passed stay filled, so the
                    flow reads as a short path with a visible end rather than an
                    open-ended interrogation. */}
                <div className="mt-2.5 flex items-center gap-1.5 border-b border-slate-200 px-3 pb-2">
                    {rail.map((s, i) => (
                        <div key={s.id} className="flex flex-1 items-center gap-1.5">
                            <div className="min-w-0 flex-1">
                                <div className="h-0.5 overflow-hidden rounded-full bg-slate-100">
                                    <motion.div
                                        className={`h-full rounded-full ${
                                            i <= railIndex ? "bg-sky-500" : "bg-transparent"
                                        }`}
                                        initial={false}
                                        animate={{ width: i <= railIndex ? "100%" : "0%" }}
                                        transition={{ duration: 0.28, ease: EASE }}
                                    />
                                </div>
                                <span
                                    className={`mt-1 block truncate text-[9.5px] uppercase tracking-[0.12em] transition-colors ${
                                        i === railIndex
                                            ? "text-slate-600"
                                            : i < railIndex
                                              ? "text-slate-400"
                                              : "text-slate-300"
                                    }`}
                                >
                                    {s.label}
                                </span>
                            </div>
                        </div>
                    ))}
                </div>

                {/* The panel resizes to whatever screen is showing rather than
                    jumping. popLayout takes the outgoing screen out of flow, so
                    the container never collapses to nothing in the gap between
                    one screen leaving and the next arriving. */}
                <motion.div layout className="relative overflow-hidden" transition={{ duration: 0.24, ease: EASE }}>
                    <AnimatePresence initial={false} mode="popLayout">
                        <motion.div
                            key={stage}
                            initial={{ opacity: 0, x: 12 * direction.current }}
                            animate={{ opacity: 1, x: 0 }}
                            exit={{ opacity: 0, x: -12 * direction.current }}
                            transition={{ duration: 0.2, ease: EASE }}
                            className="max-h-[54vh] overflow-y-auto px-3 py-3"
                        >
                            {stage === "why" ? (
                                <WhyStep finding={finding} evidence={evidence} />
                            ) : stage === "change" ? (
                                <ChangeStep
                                    finding={finding}
                                    steps={steps}
                                    // The deep link is the answer only when there
                                    // is nothing to click here; alongside a
                                    // preview it just competes with the fix.
                                    link={action ? null : link}
                                    failed={failed}
                                />
                            ) : (
                                <DoneStep finding={finding} />
                            )}
                        </motion.div>
                    </AnimatePresence>
                </motion.div>

                <div className="flex shrink-0 items-center justify-between gap-2 border-t border-slate-200 px-3 py-2.5">
                    <div>
                        {stage === "change" && !applying ? (
                            <button
                                type="button"
                                onClick={() => go("why", -1)}
                                className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-[12.5px] text-slate-500 transition hover:bg-slate-100 hover:text-slate-700"
                            >
                                <ArrowLeftIcon className="h-3 w-3" />
                                Back
                            </button>
                        ) : null}
                    </div>

                    <div className="flex items-center gap-2">
                        {stage === "done" && finding.action?.undo ? (
                            <button
                                type="button"
                                onClick={() => void undo.mutateAsync(finding.id).then(onClose)}
                                disabled={undo.isPending}
                                className="inline-flex h-7 items-center gap-1.5 rounded-md border border-slate-200 px-2.5 text-[12.5px] text-slate-600 transition hover:bg-slate-50 disabled:opacity-50"
                            >
                                {undo.isPending ? (
                                    <Loader2Icon className="h-3 w-3 animate-spin" />
                                ) : (
                                    <Undo2Icon className="h-3 w-3" />
                                )}
                                Undo
                            </button>
                        ) : null}

                        {stage === "why" ? (
                            <button
                                type="button"
                                onClick={() => go("change", 1)}
                                className="inline-flex h-7 items-center gap-1.5 rounded-md bg-sky-600 px-2.5 text-[12.5px] font-medium text-white transition hover:bg-sky-700"
                            >
                                {action ? "See what changes" : "How to fix it"}
                                <ArrowRightIcon className="h-3 w-3" />
                            </button>
                        ) : stage === "change" && action ? (
                            <button
                                type="button"
                                onClick={runFix}
                                disabled={applying}
                                className="inline-flex h-7 items-center gap-1.5 rounded-md bg-sky-600 px-2.5 text-[12.5px] font-medium text-white transition hover:bg-sky-700 disabled:opacity-60"
                            >
                                {applying ? (
                                    <Loader2Icon className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                    <CheckIcon className="h-3.5 w-3.5" />
                                )}
                                {applying ? "Applying…" : action.label}
                            </button>
                        ) : (
                            <button
                                type="button"
                                onClick={onClose}
                                className="inline-flex h-7 items-center rounded-md bg-slate-900 px-2.5 text-[12.5px] font-medium text-white transition hover:bg-slate-800"
                            >
                                Done
                            </button>
                        )}
                    </div>
                </div>
            </motion.div>
        </motion.div>
    );
}

/* ── screens ─────────────────────────────────────────── */

function WhyStep({
    finding,
    evidence,
}: {
    finding: AdvisorFinding;
    evidence: [string, unknown][];
}) {
    return (
        <div className="space-y-3">
            <p className="text-[12.5px] leading-relaxed text-slate-600">{finding.detail}</p>

            {evidence.length > 0 ? (
                <div>
                    <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                        What we measured
                    </p>
                    <dl className="mt-1.5 grid grid-cols-2 gap-1.5">
                        {evidence.map(([key, value], i) => (
                            <motion.div
                                key={key}
                                initial={{ opacity: 0, y: 4 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ delay: 0.04 + i * 0.03, duration: 0.18 }}
                                className="rounded-md border border-slate-200 bg-slate-50/60 px-2 py-1.5"
                            >
                                <dt className="truncate text-[10.5px] text-slate-500">
                                    {evidenceLabel(key)}
                                </dt>
                                <dd className="mt-0.5 text-[13px] font-medium tabular-nums text-slate-900">
                                    {evidenceValue(value)}
                                </dd>
                            </motion.div>
                        ))}
                    </dl>
                </div>
            ) : null}
        </div>
    );
}

function ChangeStep({
    finding,
    steps,
    link,
    failed,
}: {
    finding: AdvisorFinding;
    steps: string[];
    link: { href: string; label: string } | null;
    failed: string | null;
}) {
    const action = finding.action;

    return (
        <div className="space-y-3">
            {action?.preview && action.preview.length > 0 ? (
                <ul className="space-y-1.5">
                    {action.preview.map((change, i) => (
                        <motion.li
                            key={change.field}
                            initial={{ opacity: 0, y: 6 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ delay: i * 0.05, duration: 0.2, ease: EASE }}
                            className="rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-2"
                        >
                            <p className="text-[11px] text-slate-500">{change.field}</p>
                            <div className="mt-1 flex items-center gap-2 text-[12.5px]">
                                <span className="rounded border border-slate-200 bg-white px-1.5 py-0.5 text-slate-500 line-through decoration-slate-300">
                                    {change.from}
                                </span>
                                <motion.span
                                    initial={{ x: -3, opacity: 0.4 }}
                                    animate={{ x: 0, opacity: 1 }}
                                    transition={{ delay: 0.12 + i * 0.05, duration: 0.24 }}
                                >
                                    <ArrowRightIcon className="h-3 w-3 shrink-0 text-slate-400" />
                                </motion.span>
                                <span className="rounded border border-emerald-200 bg-emerald-50 px-1.5 py-0.5 font-medium text-emerald-700">
                                    {change.to}
                                </span>
                            </div>
                        </motion.li>
                    ))}
                </ul>
            ) : steps.length === 0 ? (
                <p className="text-[12.5px] leading-relaxed text-slate-700">{finding.remedy}</p>
            ) : (
                // No one-click fix, so this is the whole answer: the ordered
                // steps to do it by hand, and a way to get to the right screen.
                <ol className="space-y-1.5">
                    {steps.map((step, i) => (
                        <motion.li
                            key={step}
                            initial={{ opacity: 0, x: -6 }}
                            animate={{ opacity: 1, x: 0 }}
                            transition={{ delay: i * 0.05, duration: 0.2, ease: EASE }}
                            className="flex items-start gap-2"
                        >
                            <span className="mt-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-slate-100 text-[10px] font-medium tabular-nums text-slate-600">
                                {i + 1}
                            </span>
                            <span className="text-[12.5px] leading-relaxed text-slate-700">{step}</span>
                        </motion.li>
                    ))}
                </ol>
            )}

            {link ? (
                <Link
                    to={link.href}
                    className="inline-flex h-7 items-center gap-1.5 rounded-md border border-slate-200 px-2.5 text-[12px] font-medium text-slate-700 transition hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                >
                    {link.label}
                    <ArrowRightIcon className="h-3 w-3" />
                </Link>
            ) : null}

            {action ? (
                <div className="flex items-start gap-2 rounded-md border border-slate-200 px-2.5 py-2">
                    <ShieldCheckIcon className="mt-0.5 h-3.5 w-3.5 shrink-0 text-slate-400" />
                    <p className="text-[11.5px] leading-relaxed text-slate-500">
                        This runs as you, with your permissions, and is written to the audit log like any
                        other change you make.
                        {action.undo ? " You can undo it right after." : ""}
                    </p>
                </div>
            ) : null}

            <AnimatePresence>
                {failed ? (
                    <motion.p
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        className="rounded-md border border-rose-200 bg-rose-50 px-2.5 py-2 text-[12px] text-rose-700"
                    >
                        {failed}
                    </motion.p>
                ) : null}
            </AnimatePresence>
        </div>
    );
}

function DoneStep({ finding }: { finding: AdvisorFinding }) {
    return (
        <div className="flex flex-col items-center py-4 text-center">
            <motion.div
                initial={{ scale: 0.6, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                transition={{ type: "spring", stiffness: 380, damping: 22 }}
                className="flex h-10 w-10 items-center justify-center rounded-full bg-emerald-50"
            >
                <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" strokeWidth={2.5}>
                    <motion.path
                        d="M5 13l4 4L19 7"
                        stroke="rgb(5 150 105)"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        initial={{ pathLength: 0 }}
                        animate={{ pathLength: 1 }}
                        transition={{ delay: 0.12, duration: 0.32, ease: EASE }}
                    />
                </svg>
            </motion.div>

            <motion.p
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.2, duration: 0.2 }}
                className="mt-2.5 text-[13px] font-medium text-slate-900"
            >
                {finding.action?.label ?? "Applied"}
            </motion.p>

            <motion.p
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.26, duration: 0.2 }}
                className="mt-1 max-w-[320px] text-[12px] leading-relaxed text-slate-500"
            >
                The next check confirms it and clears the flag. Until then the row stays marked so you
                can undo without hunting for it.
            </motion.p>
        </div>
    );
}
