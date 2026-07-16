// Contact research tab: run AI research on a contact and show the latest cited
// findings (company + person summary, signals with linked sources and
// confidence chips, hooks with copy buttons, notes). Runs charge AI credits and
// never send anything; findings are read-only research material.

import React from "react";
import toast from "react-hot-toast";
import {
    SparklesIcon,
    Loader2Icon,
    ExternalLinkIcon,
    CopyIcon,
    CheckIcon,
    SearchXIcon,
} from "lucide-react";
import {
    useContactResearch,
    useRunContactResearch,
} from "@/lib/api/hooks/app/contacts/useContactResearch";
import type {
    ContactResearchRun,
    ResearchSignal,
} from "@/lib/api/models/app/contacts/Research";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { TextInput } from "@/components/ui/field";

export default function ResearchTab({ contactId }: { contactId: string }) {
    const research = useContactResearch(contactId);
    const run = useRunContactResearch(contactId);
    const [objective, setObjective] = React.useState("");

    const runs = research.data?.data ?? [];
    const latest = runs.find((r) => r.status === "succeeded" || r.status === "nothing_found");
    const inFlight = runs.some((r) => r.status === "queued" || r.status === "running");

    async function runResearch() {
        try {
            await toast.promise(run.mutateAsync(objective.trim()), {
                loading: "Researching…",
                success: "Research complete",
                error: (e: AppError) => buildError(e),
            });
            setObjective("");
        } catch {
            /* surfaced */
        }
    }

    return (
        <div className="px-4 py-4 space-y-4">
            {/* Run control */}
            <div className="rounded-md border border-slate-200 p-3">
                <div className="flex items-center gap-1.5 text-[12.5px] font-medium text-slate-900">
                    <SparklesIcon className="w-3.5 h-3.5 text-sky-600" />
                    Research this contact
                </div>
                <p className="text-[11.5px] text-slate-500 mt-1 leading-relaxed">
                    Warmbly searches the public web for current, cited facts about this person and
                    their company. Costs 2 AI credits per run.
                </p>
                <div className="mt-2.5 flex items-center gap-2">
                    <TextInput
                        value={objective}
                        onChange={setObjective}
                        placeholder="Optional focus, e.g. recent funding or hiring"
                        className="flex-1"
                        onKeyDown={(e) => {
                            if (e.key === "Enter") runResearch();
                        }}
                    />
                    <button
                        type="button"
                        onClick={runResearch}
                        disabled={run.isPending || inFlight}
                        className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                    >
                        {run.isPending || inFlight ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <SparklesIcon className="w-3 h-3" />
                        )}
                        {run.isPending || inFlight ? "Researching" : "Run research"}
                    </button>
                </div>
            </div>

            {research.isPending ? (
                <div className="h-24 rounded bg-slate-100 animate-pulse" />
            ) : !latest ? (
                <EmptyResearch />
            ) : (
                <ResearchView run={latest} />
            )}
        </div>
    );
}

function ResearchView({ run }: { run: ContactResearchRun }) {
    const r = run.result;
    if (run.status === "nothing_found" || r.nothing_found) {
        return (
            <div className="rounded-md border border-slate-200 p-3">
                <div className="flex items-center gap-1.5 text-[12.5px] font-medium text-slate-700">
                    <SearchXIcon className="w-3.5 h-3.5 text-slate-400" />
                    Nothing solid found
                </div>
                {r.research_notes && (
                    <p className="text-[11.5px] text-slate-500 mt-1 leading-relaxed">{r.research_notes}</p>
                )}
            </div>
        );
    }
    return (
        <div className="space-y-4">
            {r.company?.summary && (
                <Block label="Company">
                    <p className="text-[12px] text-slate-700 leading-relaxed">{r.company.summary}</p>
                    <div className="mt-1.5 flex flex-wrap gap-1.5 text-[10.5px]">
                        {r.company.industry && <Chip>{r.company.industry}</Chip>}
                        {r.company.size_estimate && <Chip>{r.company.size_estimate}</Chip>}
                        {(r.company.tech_or_stack_signals ?? []).map((t) => (
                            <Chip key={t}>{t}</Chip>
                        ))}
                    </div>
                </Block>
            )}

            {(r.signals?.length ?? 0) > 0 && (
                <Block label="Signals">
                    <div className="space-y-2">
                        {r.signals!.map((s, i) => (
                            <SignalRow key={i} signal={s} />
                        ))}
                    </div>
                </Block>
            )}

            {(r.hooks?.length ?? 0) > 0 && (
                <Block label="Openers">
                    <div className="space-y-2">
                        {r.hooks!.map((h, i) => (
                            <HookRow key={i} line={h.opener_line} why={h.why_relevant} />
                        ))}
                    </div>
                </Block>
            )}

            {r.research_notes && (
                <Block label="Notes">
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">{r.research_notes}</p>
                </Block>
            )}
        </div>
    );
}

function SignalRow({ signal }: { signal: ResearchSignal }) {
    return (
        <div className="rounded-md border border-slate-200 bg-slate-50/50 px-2.5 py-2">
            <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                    <span className="text-[10px] uppercase tracking-[0.08em] text-slate-400">{signal.type}</span>
                    <p className="text-[12px] text-slate-800 leading-snug">{signal.fact}</p>
                </div>
                <ConfidenceChip level={signal.confidence} />
            </div>
            <div className="mt-1 flex items-center gap-2 text-[10.5px] text-slate-400">
                {signal.when && <span>{signal.when}</span>}
                <a
                    href={signal.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-sky-600 hover:text-sky-700 truncate"
                >
                    <ExternalLinkIcon className="w-3 h-3 shrink-0" />
                    source
                </a>
            </div>
        </div>
    );
}

function HookRow({ line, why }: { line: string; why?: string }) {
    const [copied, setCopied] = React.useState(false);
    function copy() {
        navigator.clipboard.writeText(line).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        });
    }
    return (
        <div className="rounded-md border border-slate-200 px-2.5 py-2">
            <div className="flex items-start justify-between gap-2">
                <p className="text-[12px] text-slate-800 leading-snug italic">"{line}"</p>
                <button
                    type="button"
                    onClick={copy}
                    title="Copy opener"
                    className="size-6 rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center shrink-0 transition-colors"
                >
                    {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                </button>
            </div>
            {why && <p className="text-[10.5px] text-slate-400 mt-1">{why}</p>}
        </div>
    );
}

function ConfidenceChip({ level }: { level: "high" | "medium" | "low" }) {
    const cls =
        level === "high"
            ? "bg-emerald-50 text-emerald-700 border-emerald-100"
            : level === "medium"
                ? "bg-amber-50 text-amber-700 border-amber-100"
                : "bg-slate-100 text-slate-500 border-slate-200";
    return (
        <span className={`shrink-0 text-[9px] uppercase tracking-[0.08em] font-semibold rounded-sm px-1 py-0.5 border ${cls}`}>
            {level}
        </span>
    );
}

function Block({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-1.5">{label}</div>
            {children}
        </div>
    );
}

function Chip({ children }: { children: React.ReactNode }) {
    return (
        <span className="rounded-sm bg-slate-100 text-slate-600 px-1.5 py-0.5 border border-slate-200">
            {children}
        </span>
    );
}

function EmptyResearch() {
    return (
        <div className="text-center py-8 px-6">
            <div className="size-9 rounded-lg bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center mx-auto mb-2">
                <SparklesIcon className="w-4 h-4" />
            </div>
            <p className="text-[12px] text-slate-500 leading-relaxed max-w-[260px] mx-auto">
                No research yet. Run research to gather cited facts and ready-to-use openers for this
                contact.
            </p>
        </div>
    );
}
