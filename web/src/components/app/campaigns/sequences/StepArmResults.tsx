// Per-arm results of a step's A/B split, counted in contacts. Each row selects
// its arm's editor. Opens, clicks and replies are credited to the step the
// contact had most recently been sent (see GetStepArmCounts on the backend).

import { PauseIcon, PlayIcon, TrophyIcon } from "lucide-react";
import type { ABStepAnalysis, ABStepArmStats } from "@/lib/api/models/app/campaigns/ABVariant";
import type { SplitArm } from "./StepSplitAllocator";

const RULE_LABEL: Record<string, string> = {
    reply_rate: "reply rate",
    open_rate: "open rate",
    click_rate: "click rate",
};

const pct = (v: number) => `${v.toFixed(1)}%`;

export default function StepArmResults({
    arms,
    statsByKey,
    analysis,
    winningRule,
    selectedKey,
    onSelect,
    onResumeOriginal,
    promoted,
}: {
    arms: SplitArm[];
    statsByKey: Map<string, ABStepArmStats>;
    analysis: ABStepAnalysis | null;
    winningRule: string;
    selectedKey: string;
    onSelect: (key: string) => void;
    // Set when the Original is paused and can be put back into the split.
    onResumeOriginal?: () => void;
    // The arm auto-promotion handed this step to, if it did.
    promoted?: { name: string; at: string } | null;
}) {
    const totalSent = arms.reduce((s, a) => s + (statsByKey.get(a.key)?.sent ?? 0), 0);
    const rule = RULE_LABEL[winningRule] ?? "reply rate";
    const winner = analysis?.winner ?? null;

    let verdict: string;
    if (totalSent === 0) {
        verdict = "Results appear as contacts are sent this step. Sends from before results were tracked per arm are not counted.";
    } else if (winner) {
        const confidence = analysis?.confidence === "high" ? "99%" : "95%";
        verdict = `${winner.name} leads on ${rule} with ${confidence} confidence.`;
    } else {
        verdict = `No winner yet on ${rule}: every arm needs the minimum sample and a significant lead.`;
    }

    return (
        <div className="border-b border-slate-200 px-2 py-2 space-y-1.5">
            <div className="flex items-center gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400">Results by contact</span>
                {promoted && (
                    <span className="inline-flex items-center gap-1 rounded-md bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-700">
                        <TrophyIcon className="w-3 h-3" />
                        Promoted {new Date(promoted.at).toLocaleDateString()}: new contacts get {promoted.name}
                    </span>
                )}
            </div>
            {totalSent > 0 && (
                <div className="overflow-x-auto">
                    <table className="w-full text-[11.5px] tabular-nums">
                        <thead>
                            <tr className="text-left text-[10.5px] text-slate-400">
                                <th className="py-1 pr-2 font-normal">Arm</th>
                                <th className="py-1 px-2 font-normal text-right">Sent</th>
                                <th className="py-1 px-2 font-normal text-right">Open</th>
                                <th className="py-1 px-2 font-normal text-right">Click</th>
                                <th className="py-1 px-2 font-normal text-right">Reply</th>
                                <th className="py-1 pl-2 font-normal text-right">Bounced</th>
                            </tr>
                        </thead>
                        <tbody>
                            {arms.map((a) => {
                                const s = statsByKey.get(a.key);
                                return (
                                    <tr
                                        key={a.key}
                                        onClick={() => onSelect(a.key)}
                                        className={`cursor-pointer border-t border-slate-100 ${
                                            selectedKey === a.key ? "bg-sky-50/60" : "hover:bg-slate-50"
                                        }`}
                                    >
                                        <td className="py-1 pr-2">
                                            <span className="inline-flex items-center gap-1.5 min-w-0">
                                                <span className={`truncate ${a.active ? "text-slate-700" : "text-slate-400"}`}>{a.name}</span>
                                                {a.winner && <TrophyIcon className="w-3 h-3 shrink-0 text-amber-600" aria-label="Winner" />}
                                                {!a.active && <PauseIcon className="w-3 h-3 shrink-0 text-slate-300" aria-label="Paused" />}
                                                {a.isOriginal && !a.active && onResumeOriginal && (
                                                    <button
                                                        type="button"
                                                        onClick={(e) => {
                                                            e.stopPropagation();
                                                            onResumeOriginal();
                                                        }}
                                                        className="inline-flex items-center gap-0.5 rounded px-1 text-[10.5px] font-medium text-sky-700 hover:bg-sky-50"
                                                    >
                                                        <PlayIcon className="w-2.5 h-2.5" /> Resume
                                                    </button>
                                                )}
                                            </span>
                                        </td>
                                        <td className="py-1 px-2 text-right text-slate-700">{(s?.sent ?? 0).toLocaleString()}</td>
                                        <td className="py-1 px-2 text-right text-emerald-600">{pct(s?.open_rate ?? 0)}</td>
                                        <td className="py-1 px-2 text-right text-violet-600">{pct(s?.click_rate ?? 0)}</td>
                                        <td className="py-1 px-2 text-right font-medium text-sky-600">{pct(s?.reply_rate ?? 0)}</td>
                                        <td className="py-1 pl-2 text-right text-rose-600">{(s?.bounced ?? 0).toLocaleString()}</td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}
            <p className="text-[11px] text-slate-400">{verdict}</p>
        </div>
    );
}
