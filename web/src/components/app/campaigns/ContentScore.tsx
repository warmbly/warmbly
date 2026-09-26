// Advisory campaign-template content check. Two passes over the same copy:
//
//   - the rules pass (/templates/score), free and re-run on the debounce the
//     composer's preview uses. Each issue carries where it is (subject or
//     body) and the exact fragments that caused it, so the panel points at the
//     words instead of restating the rule;
//   - the AI pass (/templates/analyze), on an explicit click because it spends
//     credits. It quotes the sentence a filter will object to and says what to
//     write instead, and returns the rules pass with it so both halves of the
//     panel are scored from one reading of the copy.
//
// Re-check re-runs whichever passes are on screen, and the panel keeps the
// previous AI score so an edit can be measured against it. Neither pass ever
// blocks saving or sending.

import * as React from "react";
import {
    AlertCircleIcon,
    AlertTriangleIcon,
    ArrowDownIcon,
    ArrowUpIcon,
    InfoIcon,
    RefreshCwIcon,
    ShieldCheckIcon,
    SparklesIcon,
    WandSparklesIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import scoreTemplate from "@/lib/api/client/app/campaigns/scoreTemplate";
import useAnalyzeTemplate from "@/lib/api/hooks/app/campaigns/useAnalyzeTemplate";
import type TemplateScore from "@/lib/api/models/app/campaigns/TemplateScore";
import type {
    CopyJudgment,
    SpamFinding,
    TemplateAnalysis,
    TemplateField,
    TemplateScoreIssue,
    TemplateScoreSpan,
} from "@/lib/api/models/app/campaigns/TemplateScore";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePermission } from "@/hooks/usePermission";
import useAiMetered from "@/hooks/useAiMetered";
import { Loading } from "@/components/loader";
import { cn } from "@/lib/utils";
import { DitherMeter, type DitherTone } from "@/components/ui/dither";

function scoreTone(score: number) {
    if (score >= 80) return { text: "text-emerald-600", meter: "emerald" as DitherTone, label: "Looks good" };
    if (score >= 50) return { text: "text-amber-600", meter: "amber" as DitherTone, label: "Could improve" };
    return { text: "text-rose-600", meter: "rose" as DitherTone, label: "Needs work" };
}

// One value that changes whenever any part of the copy does, so an analysis
// can be told apart from the draft it was run against.
function copyKey(subject: string, bodyHtml: string, bodyPlain: string): string {
    return [subject, bodyHtml, bodyPlain].join("\u0000");
}

const FIELD_LABEL: Record<TemplateField, string> = { subject: "Subject", body: "Body" };

// The first thing a writer needs is which box to open.
function FieldBadge({ field, line, label }: { field?: TemplateField; line?: number; label?: string }) {
    const text = label ?? (field ? FIELD_LABEL[field] : "");
    if (!text) return null;
    return (
        <span className="shrink-0 inline-flex items-center gap-1 rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-slate-500">
            {text}
            {field === "body" && !!line && line > 1 && <span className="text-slate-400">line {line}</span>}
        </span>
    );
}

// An issue with no single field but fragments in both halves is in both, and
// saying so beats saying nothing on the one panel that exists to say where.
function issueFieldLabel(issue: TemplateScoreIssue): string | undefined {
    if (issue.field) return FIELD_LABEL[issue.field];
    const fields = new Set((issue.spans ?? []).map((s) => s.field));
    return fields.size > 1 ? "Subject and body" : undefined;
}

// The offending words, quoted exactly as they are written in the copy.
function Quoted({ text }: { text: string }) {
    return (
        <span className="rounded bg-amber-50 px-1 py-px font-mono text-[11px] text-amber-800 ring-1 ring-amber-200/70">
            {text}
        </span>
    );
}

function SpanList({ spans }: { spans: TemplateScoreSpan[] }) {
    if (spans.length === 0) return null;
    // Only when they straddle: repeating "body" on every chip of a body-only
    // issue is noise the badge above already carries.
    const mixed = new Set(spans.map((s) => s.field)).size > 1;
    return (
        <div className="mt-1 flex flex-wrap items-center gap-1">
            {spans.map((s, i) => (
                <span key={`${s.field}-${s.text}-${i}`} className="inline-flex items-center gap-1">
                    {mixed && <span className="text-[10px] uppercase tracking-[0.08em] text-slate-400">{FIELD_LABEL[s.field]}</span>}
                    <Quoted text={s.text} />
                </span>
            ))}
        </div>
    );
}

// Exported for the placement test detail, which shows the same rules pass.
export function IssueRow({ issue }: { issue: TemplateScoreIssue }) {
    const high = issue.severity === "high";
    const Icon = high ? AlertCircleIcon : AlertTriangleIcon;
    const spans = issue.spans ?? [];
    const excerpt = spans[0]?.excerpt;
    return (
        <li className="flex items-start gap-2 py-1.5">
            <Icon className={cn("w-3.5 h-3.5 shrink-0 mt-0.5", high ? "text-rose-500" : "text-amber-500")} />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                    <FieldBadge field={issue.field} label={issueFieldLabel(issue)} />
                    <span className="text-[12px] text-slate-700 leading-relaxed">{issue.message}</span>
                    <span className="text-[10px] text-slate-400 font-mono">{issue.code}</span>
                </div>
                <SpanList spans={spans} />
                {excerpt && excerpt !== spans[0]?.text && (
                    <p className="mt-1 truncate text-[11px] italic text-slate-400">{excerpt}</p>
                )}
                {issue.suggestion && <p className="mt-1 text-[11.5px] text-slate-500">{issue.suggestion}</p>}
            </div>
        </li>
    );
}

const FINDING_ICON = {
    high: { Icon: AlertCircleIcon, className: "text-rose-500" },
    warn: { Icon: AlertTriangleIcon, className: "text-amber-500" },
    info: { Icon: InfoIcon, className: "text-sky-500" },
} as const;

function FindingRow({ finding }: { finding: SpamFinding }) {
    const { Icon, className } = FINDING_ICON[finding.severity] ?? FINDING_ICON.warn;
    return (
        <li className="flex items-start gap-2 py-1.5">
            <Icon className={cn("w-3.5 h-3.5 shrink-0 mt-0.5", className)} />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                    <FieldBadge field={finding.field} line={finding.line} />
                    {finding.text && <Quoted text={finding.text} />}
                </div>
                <p className="mt-1 text-[12px] leading-relaxed text-slate-700">{finding.issue}</p>
                {finding.suggestion && (
                    <p className="mt-1 text-[11.5px] leading-relaxed text-emerald-700">Try: {finding.suggestion}</p>
                )}
            </div>
        </li>
    );
}

// Movement since the previous check, which is what makes Re-check worth
// pressing: it answers whether the edit helped.
// Thresholds mirror the backend policy (internal/app/copyjudge): a spam claim
// counts from 0.7, and a three-level scale splits at thirds.
const SPAM_CLAIM_AT = 0.7;
const BULK_AT = 0.75;
const JUDGMENT_CONF_FLOOR = 0.7;

// Mirrors copyjudge.Verdict.ReadsAsBulk on the backend.
function readsAsBulk(j: { reads_as: number; spam_claim: number; confidence: number }): boolean {
    return (j.reads_as >= BULK_AT && j.confidence >= JUDGMENT_CONF_FLOOR) || j.spam_claim >= SPAM_CLAIM_AT;
}

function readsAsLabel(v: number): { label: string; tone: DitherTone; text: string } {
    if (v < 1 / 3) return { label: "Personal note", tone: "emerald", text: "text-emerald-600" };
    if (v < 2 / 3) return { label: "Somewhere between", tone: "amber", text: "text-amber-600" };
    return { label: "Bulk mail", tone: "rose", text: "text-rose-600" };
}

function personalizationLabel(v: number): { label: string; tone: DitherTone; text: string } {
    if (v < 0.5) return { label: "Written for this reader", tone: "emerald", text: "text-emerald-600" };
    return { label: "Could be sent to anyone", tone: "amber", text: "text-amber-600" };
}

const ASK_LABEL: Record<CopyJudgment["ask"], { label: string; text: string }> = {
    one_clear_ask: { label: "One clear ask", text: "text-emerald-600" },
    several_asks: { label: "Several asks", text: "text-amber-600" },
    no_ask: { label: "No ask", text: "text-amber-600" },
};

// One row of the judgment: a name, a meter for scaled answers, and the band it
// landed in. Meters run from the personal end, so a low fraction is the good
// one and the tone carries the reading.
function JudgmentRow({
    name,
    label,
    text,
    frac,
    tone,
}: {
    name: string;
    label: string;
    text: string;
    frac?: number;
    tone?: DitherTone;
}) {
    return (
        <div className="flex items-center gap-2">
            <span className="w-24 shrink-0 text-[11.5px] text-slate-500">{name}</span>
            {frac !== undefined && tone && (
                <DitherMeter frac={Math.max(0, Math.min(1, frac))} tone={tone} height={4} className="flex-1" />
            )}
            <span className={cn("ml-auto shrink-0 text-[11.5px] font-medium", text)}>{label}</span>
        </div>
    );
}

// How the copy reads to its recipient. Numbers from a calibrated model, so the
// panel shows the band each one landed in rather than restating a verdict.
function JudgmentBlock({ judgment }: { judgment: CopyJudgment }) {
    const reads = readsAsLabel(judgment.reads_as);
    const personal = personalizationLabel(judgment.personalization);
    const ask = ASK_LABEL[judgment.ask] ?? ASK_LABEL.no_ask;
    const unsure = judgment.confidence < 0.7;
    return (
        <div className="mt-2 rounded-md border border-slate-200 bg-slate-50/60 p-2">
            <div className="flex items-center gap-1.5">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Reads as</span>
                {unsure && <span className="text-[10px] text-slate-400">low confidence</span>}
            </div>
            <div className="mt-1.5 space-y-1.5">
                <JudgmentRow name="To the reader" label={reads.label} text={reads.text} frac={judgment.reads_as} tone={reads.tone} />
                <JudgmentRow
                    name="Personalization"
                    label={personal.label}
                    text={personal.text}
                    frac={judgment.personalization}
                    tone={personal.tone}
                />
                <JudgmentRow name="Ask" label={ask.label} text={ask.text} />
            </div>
            {judgment.spam_claim >= SPAM_CLAIM_AT && (
                <p className="mt-1.5 inline-flex items-center gap-1.5 text-[11.5px] text-rose-600">
                    <AlertTriangleIcon className="w-3.5 h-3.5" />
                    Makes a claim a spam filter would object to.
                </p>
            )}
        </div>
    );
}

function ScoreDelta({ from, to }: { from: number; to: number }) {
    const diff = to - from;
    if (diff === 0) return <span className="text-[11px] text-slate-400">No change since your last check.</span>;
    const up = diff > 0;
    const Icon = up ? ArrowUpIcon : ArrowDownIcon;
    return (
        <span
            className={cn(
                "inline-flex items-center gap-0.5 text-[11px] font-medium",
                up ? "text-emerald-600" : "text-rose-600",
            )}
        >
            <Icon className="w-3 h-3" />
            {up ? "+" : ""}
            {diff} since your last check
        </span>
    );
}

export default function ContentScore({
    subject,
    bodyHtml,
    bodyPlain,
    onApplySubject,
}: {
    subject: string;
    bodyHtml: string;
    bodyPlain: string;
    // Lets the suggested subject be applied in one click. Without it the
    // suggestion still shows, it just has to be copied by hand.
    onApplySubject?: (subject: string) => void;
}) {
    const [data, setData] = React.useState<TemplateScore | null>(null);
    const [pending, setPending] = React.useState(false);
    const [failed, setFailed] = React.useState(false);
    const [analysis, setAnalysis] = React.useState<TemplateAnalysis | null>(null);
    // The copy the analysis was run against, so the panel can say when it has
    // gone stale rather than presenting old findings as current.
    const [analyzedCopy, setAnalyzedCopy] = React.useState("");
    const [previousAiScore, setPreviousAiScore] = React.useState<number | null>(null);
    // A deployment with no provider configured answers 503 once; the button
    // then stays out of the way instead of offering something that cannot run.
    const [aiUnavailable, setAiUnavailable] = React.useState(false);

    const canAI = usePermission("USE_AI");
    const metered = useAiMetered();
    const analyzeMut = useAnalyzeTemplate();

    const empty = !subject.trim() && !bodyPlain.trim();
    const currentCopy = copyKey(subject, bodyHtml, bodyPlain);
    const stale = !!analysis && analyzedCopy !== currentCopy;

    // Only the newest rules request may write the panel: a manual Re-check and
    // a debounced keystroke can be in flight together, and the slower one
    // landing last would show a score for copy that is no longer there.
    const runID = React.useRef(0);
    // What is in the editor right now, readable from a callback that closed
    // over an older draft. An analysis takes seconds, and the writer can edit
    // while it runs.
    const latestCopy = React.useRef(currentCopy);
    latestCopy.current = currentCopy;
    const runScore = React.useCallback(() => {
        const id = ++runID.current;
        setPending(true);
        scoreTemplate({ subject, body_html: bodyHtml, body_plain: bodyPlain })
            .then((res) => {
                if (id !== runID.current) return;
                setData(res);
                setFailed(false);
            })
            .catch(() => {
                if (id === runID.current) setFailed(true);
            })
            .finally(() => {
                if (id === runID.current) setPending(false);
            });
    }, [bodyHtml, bodyPlain, subject]);

    React.useEffect(() => {
        // A step with nothing written yet is not a content problem, so hold the
        // panel quiet rather than scoring an empty draft as spam.
        if (empty) {
            // Retires any in-flight request too, so its result cannot land.
            runID.current++;
            setData(null);
            setPending(false);
            setFailed(false);
            return;
        }
        // Fired inside the timer so the spinner marks a request, not a keystroke.
        const t = setTimeout(runScore, 600);
        return () => clearTimeout(t);
    }, [empty, runScore]);

    const runAnalysis = React.useCallback(() => {
        if (empty || analyzeMut.isPending) return;
        const copy = copyKey(subject, bodyHtml, bodyPlain);
        analyzeMut.mutate(
            { subject, body_html: bodyHtml, body_plain: bodyPlain },
            {
                onSuccess: (res) => {
                    // The score being replaced is what the new one is measured
                    // against, so it is captured before the swap.
                    setPreviousAiScore(analysis ? analysis.score : null);
                    setAnalysis(res);
                    setAnalyzedCopy(copy);
                    // One request scored both passes, so the rules half comes
                    // free with it. Only while the draft has not moved on: the
                    // writer can edit during an analysis, and adopting the old
                    // rules score would both show the wrong number and retire
                    // the newer request that was about to produce the right
                    // one, leaving nothing to retry it. The analysis itself is
                    // still kept, and the panel marks it stale.
                    if (copy === latestCopy.current) {
                        runID.current++;
                        setData(res.rules);
                        setPending(false);
                        setFailed(false);
                    }
                },
                onError: (e) => {
                    const err = e as unknown as AppError;
                    if (err?.status === 402) {
                        toast.error("You're out of AI credits. Add more to keep using AI analysis.");
                    } else if (err?.code === "ai_not_configured") {
                        // Permanent for this deployment, unlike a provider
                        // outage, so the button goes away rather than staying
                        // there to fail again.
                        setAiUnavailable(true);
                    } else {
                        toast.error(buildError(err));
                    }
                },
            },
        );
    }, [analysis, analyzeMut, bodyHtml, bodyPlain, empty, subject]);

    // Re-check runs everything currently on screen: the rules pass always, and
    // the AI pass too once the writer has asked for one.
    const aiInPlay = !!analysis && canAI && !aiUnavailable;
    const recheck = React.useCallback(() => {
        if (empty) return;
        if (aiInPlay) {
            runAnalysis();
            return;
        }
        runScore();
    }, [aiInPlay, empty, runAnalysis, runScore]);

    const tone = data ? scoreTone(data.score) : null;
    const aiTone = analysis ? scoreTone(analysis.score) : null;
    const busy = pending || analyzeMut.isPending;
    const suggestedSubject = analysis?.suggested_subject?.trim();

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-3 px-3 py-2.5">
                <div className="min-w-0">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Content check</div>
                    <p className="mt-0.5 text-[11px] text-slate-400 leading-relaxed">
                        Advisory deliverability score. It never blocks sending.
                    </p>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                    {busy && <Loading className="!w-3.5 h-3.5 shrink-0" />}
                    <button
                        type="button"
                        onClick={recheck}
                        disabled={empty || busy}
                        title={empty ? "Write a subject or body first" : "Score this copy again"}
                        className={cn(
                            "h-7 px-2 inline-flex items-center gap-1.5 rounded-md border text-[11.5px] font-medium transition-colors",
                            "disabled:opacity-40 disabled:cursor-not-allowed",
                            stale
                                ? "border-sky-300 bg-sky-50 text-sky-700 hover:bg-sky-100"
                                : "border-slate-200 text-slate-600 hover:bg-slate-50",
                        )}
                    >
                        <RefreshCwIcon className={cn("w-3.5 h-3.5", busy && "animate-spin")} />
                        {aiInPlay ? "Re-check with AI" : "Re-check"}
                    </button>
                </div>
            </div>

            {failed && <div className="px-3 pb-3 text-[11.5px] text-rose-600">Couldn&apos;t score this template.</div>}

            {data && tone && (
                <div className="px-3 pb-3 border-t border-slate-200/60 pt-3">
                    <div className="flex items-center gap-2">
                        <span className={cn("text-[22px] font-light leading-none tabular-nums", tone.text)}>{data.score}</span>
                        <span className="text-[11px] text-slate-400 mb-0.5">/ 100</span>
                        <span className={cn("ml-auto text-[11px] font-medium", tone.text)}>{tone.label}</span>
                    </div>
                    <DitherMeter
                        frac={Math.max(0, Math.min(100, data.score)) / 100}
                        tone={tone.meter}
                        height={6}
                        className="mt-2"
                    />
                    {data.issues.length > 0 ? (
                        <ul className="mt-2 divide-y divide-slate-200/60">
                            {data.issues.map((issue, i) => (
                                <IssueRow key={`${issue.code}-${i}`} issue={issue} />
                            ))}
                        </ul>
                    ) : (
                        <p className="mt-2 inline-flex items-center gap-1.5 text-[11.5px] text-emerald-600">
                            <ShieldCheckIcon className="w-3.5 h-3.5" /> No content issues found.
                        </p>
                    )}
                </div>
            )}

            {canAI && !aiUnavailable && !analysis && (
                <div className="border-t border-slate-200/60 px-3 py-2.5">
                    <button
                        type="button"
                        onClick={runAnalysis}
                        disabled={empty || analyzeMut.isPending}
                        className="w-full h-7 inline-flex items-center justify-center gap-1.5 rounded-md border border-slate-200 text-[11.5px] font-medium text-slate-700 transition-colors hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed"
                    >
                        <SparklesIcon className="w-3.5 h-3.5 text-sky-500" />
                        Analyze with AI
                        {metered && <span className="text-slate-400">2 credits</span>}
                    </button>
                    <p className="mt-1.5 text-[11px] leading-relaxed text-slate-400">
                        Reads the copy and names the words and sentences that hurt deliverability, in the subject and in
                        the body.
                    </p>
                </div>
            )}

            {aiUnavailable && !analysis && (
                <div className="border-t border-slate-200/60 px-3 py-2.5 text-[11.5px] text-slate-500">
                    AI analysis is not configured on this deployment.
                </div>
            )}

            {analysis && aiTone && (
                <div className="border-t border-slate-200/60 px-3 py-3">
                    <div className="flex items-center gap-1.5">
                        <SparklesIcon className="w-3.5 h-3.5 text-sky-500" />
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">AI analysis</span>
                        <span className="ml-auto truncate text-[10px] font-mono text-slate-300">{analysis.model}</span>
                    </div>

                    <div className="mt-2 flex items-center gap-2">
                        <span className={cn("text-[22px] font-light leading-none tabular-nums", aiTone.text)}>
                            {analysis.score}
                        </span>
                        <span className="text-[11px] text-slate-400 mb-0.5">/ 100</span>
                        <span className={cn("ml-auto text-[11px] font-medium", aiTone.text)}>{aiTone.label}</span>
                    </div>
                    <DitherMeter
                        frac={Math.max(0, Math.min(100, analysis.score)) / 100}
                        tone={aiTone.meter}
                        height={6}
                        className="mt-2"
                    />
                    {previousAiScore !== null && (
                        <div className="mt-1.5">
                            <ScoreDelta from={previousAiScore} to={analysis.score} />
                        </div>
                    )}

                    {stale && (
                        <p className="mt-2 text-[11.5px] text-sky-700">
                            You&apos;ve edited the copy since this analysis. Re-check to score the new version.
                        </p>
                    )}

                    {analysis.verdict && (
                        <p className="mt-2 text-[12px] leading-relaxed text-slate-600">{analysis.verdict}</p>
                    )}

                    {analysis.judgment && <JudgmentBlock judgment={analysis.judgment} />}

                    {analysis.findings.length > 0 ? (
                        <ul className="mt-1.5 divide-y divide-slate-200/60">
                            {analysis.findings.map((finding, i) => (
                                <FindingRow key={`${finding.field}-${finding.text ?? ""}-${i}`} finding={finding} />
                            ))}
                        </ul>
                    ) : (
                        // Silent when the judgment already says otherwise: an
                        // all-clear under a bulk-mail reading contradicts it.
                        !(analysis.judgment && readsAsBulk(analysis.judgment)) && (
                            <p className="mt-2 inline-flex items-center gap-1.5 text-[11.5px] text-emerald-600">
                                <ShieldCheckIcon className="w-3.5 h-3.5" /> Nothing in this copy stood out as spammy.
                            </p>
                        )
                    )}

                    {suggestedSubject && suggestedSubject !== subject.trim() && (
                        <div className="mt-2 rounded-md border border-slate-200 bg-slate-50/60 p-2">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Suggested subject
                            </div>
                            <p className="mt-1 text-[12px] leading-relaxed text-slate-700">{suggestedSubject}</p>
                            {onApplySubject && (
                                <button
                                    type="button"
                                    onClick={() => onApplySubject(suggestedSubject)}
                                    className="mt-1.5 h-7 px-2 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[11.5px] font-medium text-slate-700 transition-colors hover:bg-slate-50"
                                >
                                    <WandSparklesIcon className="w-3.5 h-3.5 text-sky-500" />
                                    Use this subject
                                </button>
                            )}
                        </div>
                    )}

                    {(analysis.improvements?.length ?? 0) > 0 && (
                        <div className="mt-2">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Also worth doing
                            </div>
                            <ul className="mt-1 space-y-1">
                                {analysis.improvements!.map((tip, i) => (
                                    <li key={i} className="flex items-start gap-1.5 text-[11.5px] leading-relaxed text-slate-600">
                                        <span className="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-slate-300" />
                                        {tip}
                                    </li>
                                ))}
                            </ul>
                        </div>
                    )}

                    {metered && analysis.credits_charged > 0 && (
                        <p className="mt-2 text-[10.5px] text-slate-400">
                            {analysis.credits_charged} credit{analysis.credits_charged === 1 ? "" : "s"} spent,{" "}
                            {analysis.credits_remaining} left
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}
