// AIEditPopover — the floating "edit this with AI" card shared by every
// composer surface (unibox textarea, campaign rich editor). Pure UI: the host
// owns selection tracking, positioning, and applying the rewrite; this renders
// the instruction input, quick actions, the busy shimmer, and the post-apply
// review row (undo / try again).

import React from "react";
import {
    ArrowUpIcon,
    CheckIcon,
    Undo2Icon,
    RefreshCwIcon,
    SparklesIcon,
    WandSparklesIcon,
    MinusIcon,
    PlusIcon,
    SpellCheckIcon,
    SmileIcon,
    BriefcaseIcon,
} from "lucide-react";

export interface AIQuickAction {
    key: string;
    label: string;
    icon: React.ReactNode;
    instruction: string;
}

export const AI_QUICK_ACTIONS: AIQuickAction[] = [
    {
        key: "improve",
        label: "Improve",
        icon: <WandSparklesIcon className="w-3 h-3" />,
        instruction:
            "Improve the writing: clearer, smoother, better flow. Keep the meaning and roughly the same length.",
    },
    {
        key: "shorten",
        label: "Shorten",
        icon: <MinusIcon className="w-3 h-3" />,
        instruction: "Make this more concise. Cut filler and keep the meaning.",
    },
    {
        key: "expand",
        label: "Expand",
        icon: <PlusIcon className="w-3 h-3" />,
        instruction: "Expand this slightly with more substance and specificity. No fluff.",
    },
    {
        key: "grammar",
        label: "Fix grammar",
        icon: <SpellCheckIcon className="w-3 h-3" />,
        instruction: "Fix spelling, grammar, and punctuation only. Change nothing else.",
    },
    {
        key: "friendlier",
        label: "Friendlier",
        icon: <SmileIcon className="w-3 h-3" />,
        instruction: "Make the tone warmer and friendlier without getting sappy.",
    },
    {
        key: "formal",
        label: "More formal",
        icon: <BriefcaseIcon className="w-3 h-3" />,
        instruction: "Make the tone more professional and polished.",
    },
];

export type AIEditPhase = "idle" | "busy" | "applied";

interface AIEditPopoverProps {
    phase: AIEditPhase;
    // Credits remaining after the last run, when known.
    credits: number | null;
    onRun: (instruction: string) => void;
    onUndo: () => void;
    onRetry: () => void;
    onDone: () => void;
}

export default function AIEditPopover({
    phase,
    credits,
    onRun,
    onUndo,
    onRetry,
    onDone,
}: AIEditPopoverProps) {
    const [instruction, setInstruction] = React.useState("");
    const inputRef = React.useRef<HTMLInputElement>(null);

    React.useEffect(() => {
        if (phase === "idle") inputRef.current?.focus();
    }, [phase]);

    const run = () => {
        const text = instruction.trim();
        if (!text) return;
        onRun(text);
    };

    if (phase === "busy") {
        return (
            <div className="w-[300px] px-3 py-2.5 flex items-center gap-2">
                <SparklesIcon className="w-3.5 h-3.5 text-sky-500 animate-pulse shrink-0" />
                <span className="ai-shimmer-text text-[12px] font-medium">Rewriting…</span>
            </div>
        );
    }

    if (phase === "applied") {
        return (
            <div className="w-[300px] px-2.5 py-2 flex items-center gap-1.5">
                <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-slate-900 mr-auto">
                    <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                    Rewritten
                    {credits !== null && (
                        <span className="text-[10.5px] font-normal text-slate-400">
                            · {credits} credit{credits === 1 ? "" : "s"} left
                        </span>
                    )}
                </span>
                <button
                    type="button"
                    onClick={onUndo}
                    className="h-6 px-1.5 rounded inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    <Undo2Icon className="w-3 h-3" />
                    Undo
                </button>
                <button
                    type="button"
                    onClick={onRetry}
                    className="h-6 px-1.5 rounded inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    <RefreshCwIcon className="w-3 h-3" />
                    Again
                </button>
                <button
                    type="button"
                    onClick={onDone}
                    className="h-6 px-2 rounded bg-slate-900 text-white text-[11.5px] font-medium hover:bg-slate-700 transition-colors"
                >
                    Done
                </button>
            </div>
        );
    }

    return (
        <div className="w-[300px]">
            <div className="flex items-center gap-1.5 px-2.5 pt-2.5">
                <SparklesIcon className="w-3.5 h-3.5 text-sky-500 shrink-0" />
                <input
                    ref={inputRef}
                    value={instruction}
                    onChange={(e) => setInstruction(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault();
                            run();
                        }
                    }}
                    placeholder="Tell AI how to change it…"
                    maxLength={2000}
                    className="flex-1 min-w-0 h-7 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                />
                <button
                    type="button"
                    onClick={run}
                    disabled={!instruction.trim()}
                    aria-label="Rewrite selection"
                    className="size-6 rounded-md bg-sky-600 text-white inline-flex items-center justify-center hover:bg-sky-700 transition-colors disabled:opacity-40"
                >
                    <ArrowUpIcon className="w-3.5 h-3.5" />
                </button>
            </div>
            <div className="px-2.5 pb-2.5 pt-2 flex flex-wrap gap-1">
                {AI_QUICK_ACTIONS.map((a) => (
                    <button
                        key={a.key}
                        type="button"
                        onClick={() => onRun(a.instruction)}
                        className="h-6 px-2 rounded-full border border-slate-200 inline-flex items-center gap-1 text-[11px] text-slate-600 hover:border-sky-300 hover:text-sky-700 hover:bg-sky-50 transition-colors"
                    >
                        {a.icon}
                        {a.label}
                    </button>
                ))}
            </div>
        </div>
    );
}
