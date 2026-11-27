// AI help: a description becomes a search, or refines the one on screen; the answer is a reviewable diff, nothing runs until applied.
import { BanIcon, CheckIcon, CornerDownLeftIcon, InfoIcon, Loader2Icon, SparklesIcon, XIcon } from "lucide-react";
import { useRef, useState, type Dispatch, type RefObject } from "react";
import { useFinderAi as useAiMutation } from "@/lib/api/hooks/app/social/useSocial";
import type { AiChange, AiResult, FinderQuery } from "@/lib/api/models/app/social/Social";
import { socialErrorCode, socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { aiChangeApplied, apiAccount, isEmpty, toQuery, type FinderAction, type FinderState } from "./state";

export const AI_EXAMPLES = [
    "Heads of growth at Series A fintechs in Germany, not agencies",
    "Founders of 11-50 person SaaS companies in the Nordics who posted lately",
    "VPs of Sales in the UK who changed jobs recently",
];

export interface FinderAi {
    prompt: string;
    setPrompt: (p: string) => void;
    run: (prompt?: string, fresh?: boolean, base?: FinderQuery) => void;
    pending: boolean;
    result: AiResult | null;
    refined: boolean;
    dismiss: () => void;
    error: unknown;
    /** No model is configured: the box hides with a note. */
    unavailable: boolean;
}

export function useFinderAi({ state, accountId }: { state: FinderState; accountId: string }): FinderAi {
    const build = useAiMutation();
    const [prompt, setPrompt] = useState("");
    const [result, setResult] = useState<AiResult | null>(null);
    const [refined, setRefined] = useState(false);
    const [unavailable, setUnavailable] = useState(false);
    // Bumped on dismiss, so an answer arriving after it is dropped.
    const attempt = useRef(0);
    return {
        prompt,
        setPrompt,
        run: (p, fresh, base) => {
            const text = (p ?? prompt).trim();
            if (!text || build.isPending) return;
            if (p !== undefined) setPrompt(p);
            const current = fresh ? null : (base ?? (!isEmpty(state) ? toQuery(state) : null));
            const mine = ++attempt.current;
            setResult(null);
            build.mutate(
                { prompt: text, source: current?.source ?? state.source, current_query: current, account_id: apiAccount(accountId) },
                {
                    onSuccess: (r) => {
                        if (mine !== attempt.current) return;
                        setResult(r);
                        setRefined(!!current);
                    },
                    onError: (e) => {
                        if (socialErrorCode(e) === "ai_unavailable") setUnavailable(true);
                    },
                },
            );
        },
        pending: build.isPending,
        result,
        refined,
        dismiss: () => {
            attempt.current++;
            setResult(null);
            build.reset();
        },
        error: unavailable ? null : build.error,
        unavailable,
    };
}

/** The prompt box; `large` is the empty state's version, with examples. */
export function AiBox({ ai, hasSearch, large, inputRef }: { ai: FinderAi; hasSearch: boolean; large?: boolean; inputRef?: RefObject<HTMLTextAreaElement | null> }) {
    const ownRef = useRef<HTMLTextAreaElement>(null);
    const ref = inputRef ?? ownRef;
    if (ai.unavailable) {
        return (
            <p className="flex items-center gap-1.5 px-3 py-2 text-[11.5px] text-slate-400">
                <InfoIcon aria-hidden className="size-3.5" /> AI help is not set up on this instance.
            </p>
        );
    }
    const refine = hasSearch && !large;
    return (
        <div className={cn("grid gap-2", large ? "mx-auto w-full max-w-2xl" : "px-3 py-3 border-b border-slate-200")}>
            <form
                className={cn("grid gap-2 rounded-md border border-slate-200 bg-white p-2 transition-colors focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100", large && "p-3", ai.pending && "border-sky-300")}
                onSubmit={(e) => {
                    e.preventDefault();
                    ai.run(undefined, large);
                }}
            >
                <label className="flex items-center gap-1.5 px-1 text-[11.5px] font-medium text-sky-700">
                    <SparklesIcon aria-hidden className={cn("size-3.5", ai.pending && "animate-pulse")} />
                    {refine ? "Refine with AI" : "Describe who you want to reach"}
                </label>
                <textarea
                    ref={ref}
                    rows={large ? 2 : 1}
                    value={ai.prompt}
                    onChange={(e) => ai.setPrompt(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                            e.preventDefault();
                            ai.run(undefined, large);
                        }
                    }}
                    placeholder={refine ? "e.g. exclude agencies, add the Nordics, only founders" : "e.g. heads of growth at Series A fintechs in Germany, not agencies"}
                    aria-label={refine ? "Refine the search with AI" : "Describe who you want to reach"}
                    className="field-sizing-content max-h-40 min-h-7 w-full resize-none bg-transparent px-1 text-[16px] md:text-[12.5px] leading-relaxed text-slate-900 outline-none placeholder:text-slate-400"
                />
                <div className="flex items-center gap-2">
                    <span className="text-[11px] text-slate-400">Uses AI credits. Nothing is searched until you apply it.</span>
                    <button
                        type="submit"
                        disabled={!ai.prompt.trim() || ai.pending}
                        className="ml-auto h-7 px-2.5 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                    >
                        {ai.pending ? <Loader2Icon className="size-3.5 animate-spin" /> : null}
                        {refine ? "Refine" : "Build search"}
                        {!ai.pending && <CornerDownLeftIcon className="size-3 opacity-70" />}
                    </button>
                </div>
            </form>
            {ai.error != null && (
                <div className="flex items-start gap-2 rounded-md border border-red-200 bg-red-50/60 px-3 py-2 text-[12px] text-red-700" role="alert">
                    <span className="flex-1">{socialErrorMessage(ai.error)}</span>
                    <button type="button" onClick={() => ai.run()} className="font-medium hover:underline">
                        Try again
                    </button>
                </div>
            )}
            {large && !ai.result && !ai.pending && ai.error == null && (
                <div className="flex flex-wrap justify-center gap-2">
                    {AI_EXAMPLES.map((ex) => (
                        <button key={ex} type="button" onClick={() => ai.run(ex, true)} className="rounded-full border border-slate-200 bg-white px-3 py-1 text-[12px] text-slate-700 hover:border-slate-300 hover:bg-slate-50">
                            {ex}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}

const ACTION_LABEL: Record<AiChange["action"], string> = { set: "Set", add: "Add", remove: "Remove", clear: "Clear" };

/** The answer as a diff of the search: apply everything, or one change at a time. */
export function AiReview({ ai, state, dispatch, onApplyAll, className }: { ai: FinderAi; state: FinderState; dispatch: Dispatch<FinderAction>; onApplyAll: (q: FinderQuery) => void; className?: string }) {
    const r = ai.result;
    if (!r) return null;
    // Per-change apply needs the same source; a new source can only be taken whole.
    const sameSource = r.query.source === state.source;
    const allApplied = sameSource && r.changes.every((c) => aiChangeApplied(state, c));
    return (
        <div className={cn("grid gap-2.5 rounded-md border border-sky-200 bg-sky-50/40 p-3", className)} role="region" aria-label="AI suggestion">
            <div className="flex items-start gap-2">
                <SparklesIcon aria-hidden className="mt-0.5 size-3.5 shrink-0 text-sky-600" />
                <p className="min-w-0 flex-1 text-[12.5px] font-medium text-slate-900">{r.summary}</p>
                <button type="button" aria-label="Dismiss the AI suggestion" onClick={ai.dismiss} className="text-slate-400 hover:text-slate-700">
                    <XIcon className="size-3.5" />
                </button>
            </div>
            {r.changes.length > 0 && (
                <ul className="grid gap-1" aria-label="Proposed changes">
                    {r.changes.map((c, i) => {
                        const applied = sameSource && aiChangeApplied(state, c);
                        return (
                            <li key={`${c.filter}:${i}`} className="flex items-start gap-2 rounded bg-white px-2 py-1.5 text-[12px] ring-1 ring-inset ring-slate-200">
                                <span className={cn("mt-px w-12 shrink-0 text-[10px] font-semibold uppercase tracking-wide", c.action === "remove" || c.action === "clear" ? "text-red-600" : "text-emerald-600")}>{ACTION_LABEL[c.action]}</span>
                                <span className="grid min-w-0 flex-1 gap-1">
                                    <span className="font-medium text-slate-800">{c.label ?? c.filter.replace(/_/g, " ")}</span>
                                    {c.values.length > 0 && (
                                        <span className="flex flex-wrap gap-1">
                                            {c.values.map((v, j) => (
                                                <span
                                                    key={j}
                                                    className={cn("inline-flex h-5 items-center gap-1 rounded-full px-1.5 text-[11px] ring-1 ring-inset", v.exclude ? "bg-red-50 text-red-700 ring-red-200" : "bg-white text-slate-700 ring-slate-200")}
                                                >
                                                    {v.exclude ? <BanIcon className="size-2.5" /> : <CheckIcon className="size-2.5" strokeWidth={3} />}
                                                    {v.text || v.id}
                                                </span>
                                            ))}
                                        </span>
                                    )}
                                </span>
                                {sameSource &&
                                    (applied ? (
                                        <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-emerald-600">
                                            <CheckIcon className="size-3" /> Applied
                                        </span>
                                    ) : (
                                        <button type="button" onClick={() => dispatch({ type: "aiChange", change: c })} className="h-6 shrink-0 px-2 rounded text-[11.5px] font-medium text-sky-700 hover:bg-sky-50">
                                            Apply
                                        </button>
                                    ))}
                            </li>
                        );
                    })}
                </ul>
            )}
            {r.unresolved.length > 0 && (
                <div className="rounded bg-amber-50 px-2.5 py-2 text-[11.5px] text-amber-800">
                    <p className="font-medium">LinkedIn had no match for:</p>
                    <ul className="mt-0.5 list-disc pl-4">
                        {r.unresolved.map((u, i) => (
                            <li key={i}>{typeof u === "string" ? u : `"${u.text}"${u.label ? ` (${u.label})` : ""}`}</li>
                        ))}
                    </ul>
                </div>
            )}
            {r.assumptions.length > 0 && (
                <ul className="grid gap-0.5 text-[11.5px] text-slate-500" aria-label="Assumptions">
                    {r.assumptions.map((a, i) => (
                        <li key={i} className="flex gap-1.5">
                            <InfoIcon aria-hidden className="mt-0.5 size-3 shrink-0" />
                            {a}
                        </li>
                    ))}
                </ul>
            )}
            <div className="flex flex-wrap items-center gap-2">
                <button
                    type="button"
                    disabled={allApplied}
                    onClick={() => onApplyAll(r.query)}
                    className="h-7 px-2.5 rounded-md bg-slate-900 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                >
                    <CheckIcon className="size-3.5" /> {allApplied ? "Applied" : ai.refined && sameSource ? "Apply all changes" : "Use this search"}
                </button>
                <button type="button" onClick={ai.dismiss} className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                    Discard
                </button>
            </div>
            {r.suggestions.length > 0 && (
                <div className="grid gap-1.5 border-t border-sky-100 pt-2">
                    <p className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Refine further</p>
                    <div className="flex flex-wrap gap-1.5">
                        {r.suggestions.map((s) => (
                            <button
                                key={s}
                                type="button"
                                disabled={ai.pending}
                                onClick={() => ai.run(s, false, allApplied ? undefined : r.query)}
                                className="rounded-full border border-slate-200 bg-white px-2.5 py-0.5 text-[11.5px] text-slate-700 hover:border-slate-300 hover:bg-slate-50 disabled:opacity-50"
                            >
                                {s}
                            </button>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}
