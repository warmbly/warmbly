// Advisory campaign-template content check. A "Check content" button POSTs the
// current subject + body to /templates/score and renders a 0-100 score (higher
// = safer) plus a list of non-blocking issues. Purely advisory — it never
// blocks saving or sending, it just surfaces deliverability hints.

import { ShieldCheckIcon, AlertTriangleIcon, AlertCircleIcon } from "lucide-react";
import useScoreTemplate from "@/lib/api/hooks/app/campaigns/useScoreTemplate";
import type { TemplateScoreIssue } from "@/lib/api/models/app/campaigns/TemplateScore";
import { Loading } from "@/components/loader";
import { cn } from "@/lib/utils";

function scoreTone(score: number) {
    if (score >= 80) return { text: "text-emerald-600", bar: "bg-emerald-500", label: "Looks good" };
    if (score >= 50) return { text: "text-amber-600", bar: "bg-amber-500", label: "Could improve" };
    return { text: "text-rose-600", bar: "bg-rose-500", label: "Needs work" };
}

function IssueRow({ issue }: { issue: TemplateScoreIssue }) {
    const high = issue.severity === "high";
    const Icon = high ? AlertCircleIcon : AlertTriangleIcon;
    return (
        <li className="flex items-start gap-2 py-1.5">
            <Icon className={cn("w-3.5 h-3.5 shrink-0 mt-0.5", high ? "text-rose-500" : "text-amber-500")} />
            <div className="min-w-0">
                <span className="text-[12px] text-slate-700 leading-relaxed">{issue.message}</span>
                <span className="ml-1.5 text-[10px] text-slate-400 font-mono">{issue.code}</span>
            </div>
        </li>
    );
}

export default function ContentScore({
    subject,
    bodyHtml,
    bodyPlain,
}: {
    subject: string;
    bodyHtml: string;
    bodyPlain: string;
}) {
    const score = useScoreTemplate();
    const data = score.data;

    const run = () =>
        score.mutate({ subject, body_html: bodyHtml, body_plain: bodyPlain });

    const tone = data ? scoreTone(data.score) : null;

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-3 px-3 py-2.5">
                <div className="min-w-0">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Content check</div>
                    <p className="mt-0.5 text-[11px] text-slate-400 leading-relaxed">Advisory deliverability score — never blocks sending.</p>
                </div>
                <button
                    type="button"
                    onClick={run}
                    disabled={score.isPending}
                    className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 shrink-0"
                >
                    {score.isPending ? <Loading className="!w-3.5 h-3.5" /> : <ShieldCheckIcon className="w-3.5 h-3.5" />}
                    {data ? "Re-check" : "Check content"}
                </button>
            </div>

            {score.isError && (
                <div className="px-3 pb-3 text-[11.5px] text-rose-600">Couldn&apos;t score this template. Try again.</div>
            )}

            {data && tone && (
                <div className="px-3 pb-3 border-t border-slate-200/60 pt-3">
                    <div className="flex items-center gap-2">
                        <span className={cn("text-[22px] font-light leading-none tabular-nums", tone.text)}>{data.score}</span>
                        <span className="text-[11px] text-slate-400 mb-0.5">/ 100</span>
                        <span className={cn("ml-auto text-[11px] font-medium", tone.text)}>{tone.label}</span>
                    </div>
                    <div className="mt-2 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                        <div className={cn("h-full rounded-full transition-all", tone.bar)} style={{ width: `${Math.max(0, Math.min(100, data.score))}%` }} />
                    </div>
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
        </div>
    );
}
