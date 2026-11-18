import React from "react";
import { Link } from "@tanstack/react-router";
import { motion } from "framer-motion";
import { AlertTriangleIcon, CheckCircle2Icon, CheckIcon, FileWarningIcon, MailXIcon, SparklesIcon, UserXIcon } from "lucide-react";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { CampaignReviewSummary, MissingField } from "@/lib/api/models/app/campaigns/Review";
import { MISSING_FIELD_LABEL } from "@/lib/api/models/app/campaigns/Review";
import { fmtUsd } from "@/lib/money";
import type { BulkAIProgress } from "./useBulkAIWrite";
import { cn } from "@/lib/utils";
import { RunningGrid } from "./reviewLook";

const plural = (n: number, one: string, many = one + "s") => `${n.toLocaleString()} ${n === 1 ? one : many}`;

type Check = { key: string; icon: React.ElementType; tone: string; text: React.ReactNode; action?: React.ReactNode; blocking?: boolean; warn?: boolean };

const CHIP = "h-7 px-2 rounded-md border inline-flex items-center gap-1.5 text-[12px] font-medium whitespace-nowrap transition-colors";
const ACTION = "shrink-0 h-6 px-2 rounded text-[11.5px] font-medium text-sky-700 hover:bg-sky-50 transition-colors";

// One line above the leads: what to check before sending, the AI copy's progress, and whether leads wait for approval.
export default function ReviewToolbar({
    campaignId,
    summary,
    canToggle,
    toggling,
    onToggleReview,
    onMissing,
    canApproveAll,
    onApproveAll,
    canWriteAI,
    aiProgress,
    onWriteAI,
    onStopAI,
}: {
    campaignId: string;
    summary: CampaignReviewSummary;
    canToggle: boolean;
    toggling: boolean;
    onToggleReview: (enabled: boolean) => void;
    onMissing: (f: MissingField) => void;
    canApproveAll: boolean;
    onApproveAll: () => void;
    canWriteAI: boolean;
    aiProgress: BulkAIProgress | null;
    onWriteAI: () => void;
    onStopAI: () => void;
}) {
    const { ai } = summary;
    const [checksOpen, setChecksOpen] = React.useState(false);

    const checks: Check[] = [];
    for (const e of summary.template_errors ?? []) {
        checks.push({
            key: `err-${e.step_id}`,
            icon: FileWarningIcon,
            tone: "text-rose-600",
            blocking: true,
            text: <>Email {e.position} will not render as written: {e.message}.</>,
            action: (
                <Link to="/app/campaigns/$id/steps" params={{ id: campaignId }} className={ACTION}>
                    Fix in Steps
                </Link>
            ),
        });
    }
    for (const m of summary.missing) {
        checks.push({
            key: `missing-${m.field}`,
            icon: AlertTriangleIcon,
            tone: "text-amber-500",
            warn: true,
            text: (
                <>
                    {plural(m.leads, "lead")} {m.leads === 1 ? "has" : "have"} no {MISSING_FIELD_LABEL[m.field] ?? m.field}, and the emails print it with nothing in its place.
                </>
            ),
            action: (
                <button
                    type="button"
                    onClick={() => {
                        setChecksOpen(false);
                        onMissing(m.field);
                    }}
                    className={ACTION}
                >
                    Show them
                </button>
            ),
        });
    }
    if (summary.unsubscribed > 0) {
        checks.push({ key: "unsub", icon: UserXIcon, tone: "text-slate-400", text: <>{plural(summary.unsubscribed, "lead")} unsubscribed. Sending skips them.</> });
    }
    if (summary.undeliverable > 0) {
        checks.push({
            key: "undeliverable",
            icon: MailXIcon,
            tone: "text-slate-400",
            text: <>{plural(summary.undeliverable, "address", "addresses")} failed verification. The launch check counts them against the list.</>,
        });
    }
    const blocking = checks.filter((c) => c.blocking).length;
    const warnings = checks.filter((c) => c.warn).length;

    const chip =
        blocking > 0
            ? { tone: "border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100/70", icon: FileWarningIcon, label: plural(blocking, "problem") }
            : warnings > 0
              ? { tone: "border-amber-200 bg-amber-50 text-amber-800 hover:bg-amber-100/70", icon: AlertTriangleIcon, label: `${plural(warnings, "thing")} to check` }
              : checks.length > 0
                ? { tone: "border-slate-200 bg-white text-slate-600 hover:border-slate-300", icon: CheckCircle2Icon, label: `Ready · ${plural(checks.length, "note")}` }
                : { tone: "border-emerald-200 bg-emerald-50 text-emerald-700 hover:bg-emerald-100/70", icon: CheckCircle2Icon, label: "Ready to send" };

    const total = ai.written + ai.pending;
    const busy = !!aiProgress || (!!ai.auto && ai.pending > 0);
    const done = aiProgress ? aiProgress.written : ai.written;
    const of = aiProgress ? aiProgress.written + aiProgress.remaining : total;
    const share = of > 0 ? done / of : 1;
    const aiText = aiProgress
        ? `Writing AI copy now: ${aiProgress.written.toLocaleString()} done, ${aiProgress.remaining.toLocaleString()} to go.`
        : ai.pending === 0
          ? ai.written > 0
              ? `AI copy in the first email is written for all ${ai.written.toLocaleString()} leads not yet emailed.`
              : "Every lead has received the first email, so there is no AI copy left to write."
          : ai.auto
            ? `Writing every lead's AI copy in the background: ${ai.written.toLocaleString()} of ${total.toLocaleString()} done.`
            : `AI copy is written for every lead once the campaign runs: ${ai.written.toLocaleString()} of ${total.toLocaleString()} written so far.`;

    return (
        <div className="shrink-0 px-3 py-2 flex flex-wrap items-center gap-2 border-b border-slate-200 bg-slate-50/50">
            <PopoverMenu open={checksOpen} onOpenChange={setChecksOpen}>
                <PopoverMenuTrigger asChild>
                    <button type="button" className={cn(CHIP, chip.tone)} title="What sending will do with these leads">
                        <chip.icon className="w-3.5 h-3.5" />
                        {chip.label}
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={340} role="dialog" aria-label="Sending checks" className="p-1 max-w-[min(420px,calc(100vw-32px))]">
                    {checks.length === 0 ? (
                        <div className="px-2.5 py-2 flex items-start gap-2 text-[12px] text-emerald-700">
                            <CheckCircle2Icon className="mt-px w-3.5 h-3.5 shrink-0" />
                            Every lead not yet emailed has everything its emails print.
                        </div>
                    ) : (
                        <ul className="divide-y divide-slate-100">
                            {checks.map((c) => (
                                <li key={c.key} className="px-2.5 py-2 flex items-start gap-2 text-[12px] leading-relaxed text-slate-700">
                                    <c.icon className={cn("mt-[3px] w-3.5 h-3.5 shrink-0", c.tone)} />
                                    <span className="min-w-0 flex-1">{c.text}</span>
                                    {c.action}
                                </li>
                            ))}
                        </ul>
                    )}
                </PopoverMenuContent>
            </PopoverMenu>

            {ai.blocks > 0 && (
                <PopoverMenu>
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            className={cn(CHIP, "border-slate-200 bg-white text-slate-700 hover:border-slate-300")}
                            title={aiText}
                        >
                            {busy ? <RunningGrid className="text-sky-600" /> : <SparklesIcon className="w-3.5 h-3.5 text-violet-500" />}
                            {!busy && ai.pending === 0 ? (
                                <>
                                    AI copy written
                                    <CheckIcon className="w-3 h-3 text-emerald-600" />
                                </>
                            ) : (
                                <>
                                    <span className={cn(busy && "text-shimmer text-sky-800")}>
                                        AI copy {done.toLocaleString()}/{of.toLocaleString()}
                                    </span>
                                    <span aria-hidden className="relative h-1 w-10 rounded-full bg-slate-100 overflow-hidden">
                                        <motion.span
                                            className="absolute inset-y-0 left-0 rounded-full bg-sky-500"
                                            initial={false}
                                            animate={{ width: `${Math.round(share * 100)}%` }}
                                            transition={{ duration: 0.4, ease: "easeOut" }}
                                        />
                                    </span>
                                </>
                            )}
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={300} role="dialog" aria-label="AI copy" className="p-2.5 max-w-[min(380px,calc(100vw-32px))]">
                        <p className="text-[12px] leading-relaxed text-slate-700">{aiText}</p>
                        {aiProgress ? (
                            <button type="button" onClick={onStopAI} className="mt-2 h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50">
                                Stop
                            </button>
                        ) : ai.pending > 0 && canWriteAI && !ai.auto ? (
                            <button
                                type="button"
                                onClick={onWriteAI}
                                title="Charged the same as writing it at send time, and never twice"
                                className="mt-2 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5"
                            >
                                Write all now
                                <span className="font-normal text-sky-100">about {fmtUsd(ai.estimated_cost_usd)}</span>
                            </button>
                        ) : null}
                    </PopoverMenuContent>
                </PopoverMenu>
            )}

            <div className="ml-auto flex items-center gap-2">
                {canApproveAll && (
                    <button
                        type="button"
                        onClick={onApproveAll}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 shadow-sm transition-colors"
                    >
                        <CheckIcon className="w-3.5 h-3.5" />
                        Approve all {summary.counts.waiting.toLocaleString()}
                    </button>
                )}
                <div
                    className="h-7 pl-2 pr-1 rounded-md inline-flex items-center gap-2 text-[12px] text-slate-600"
                    title={
                        summary.review_leads
                            ? "New leads wait here until you approve them. Follow-ups after the first email go out on their own."
                            : "Off: every lead starts on schedule. You can still read and personalize each lead's emails."
                    }
                >
                    <span className="hidden sm:inline">Approve before sending</span>
                    <Toggle
                        value={summary.review_leads}
                        onChange={onToggleReview}
                        disabled={!canToggle || toggling}
                        ariaLabel="Approve before sending"
                    />
                </div>
            </div>
        </div>
    );
}
