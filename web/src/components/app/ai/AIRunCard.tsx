// AIRunCard is what floats by an editor while AI works on it: a status pill
// that follows the real stage (and stops on demand), then a review menu once
// the text is in. The menu takes a follow-up instruction or a pick from the
// list, by mouse or by arrow keys: Keep, Try again, a few one-tap changes, and
// Discard, which puts the document back the way it was.

import React from "react";
import { ArrowUpIcon, CheckIcon, RefreshCwIcon, SparklesIcon, Trash2Icon } from "lucide-react";
import { Kbd } from "@/components/ui/shortcut-tooltip";
import useFocusOnOpen from "@/hooks/useFocusOnOpen";
import AIBusyStatus from "./AIBusyStatus";
import type { AIRefinement } from "./aiCard";
import { AI_CARD_WIDTH } from "./floatingBounds";
import formatUsage from "./usage";
import type { EditorAIRun } from "./useEditorAIRun";

export default function AIRunCard({
    run,
    thinking,
    writing,
    refinements,
}: {
    run: EditorAIRun;
    // Labels for the wait before the first word and for the writing itself.
    thinking: string;
    writing: string;
    refinements: AIRefinement[];
}) {
    if (run.phase === "idle") return null;
    if (run.phase !== "review") {
        const label = run.phase === "thinking" ? thinking : run.phase === "writing" ? writing : "Checking it against spam filters…";
        return (
            <AIBusyStatus
                labels={[label]}
                onStop={run.phase === "writing" ? run.stop : undefined}
                onCancel={run.phase === "writing" ? undefined : run.discard}
            />
        );
    }
    return <ReviewMenu run={run} refinements={refinements} />;
}

function ReviewMenu({ run, refinements }: { run: EditorAIRun; refinements: AIRefinement[] }) {
    const [instruction, setInstruction] = React.useState("");
    const [active, setActive] = React.useState(0);
    const inputRef = React.useRef<HTMLInputElement>(null);
    useFocusOnOpen(true, inputRef);

    const items: { key: string; label: string; icon: React.ReactNode; act: () => void; hint?: string; danger?: boolean }[] = [
        { key: "keep", label: "Keep it", icon: <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />, hint: "mod+enter", act: run.keep },
        { key: "retry", label: "Try again", icon: <RefreshCwIcon className="w-3.5 h-3.5 text-slate-400" />, act: run.retry },
        ...refinements.map((r) => ({ key: r.label, label: r.label, icon: r.icon, act: () => run.refine(r.instruction) })),
        { key: "discard", label: "Discard", icon: <Trash2Icon className="w-3.5 h-3.5 text-slate-400" />, hint: "esc", act: run.discard, danger: true },
    ];

    const submit = () => {
        const text = instruction.trim();
        if (text) run.refine(text);
        else items[active]?.act();
    };

    const usage = run.usage ? formatUsage(run.usage.chargedUsd, run.usage.tokens) : "";
    const summary = run.changed
        ? `${run.words} word${run.words === 1 ? "" : "s"} written`
        : "No change: it came back as it was";

    return (
        <div style={{ width: AI_CARD_WIDTH }} role="dialog" aria-label="Review the AI text">
            <div className="flex items-center gap-1.5 px-2.5 pt-2.5 pb-2">
                <SparklesIcon className="w-3.5 h-3.5 shrink-0 text-sky-500" />
                <input
                    ref={inputRef}
                    value={instruction}
                    onChange={(e) => setInstruction(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                            e.preventDefault();
                            run.keep();
                        } else if (e.key === "Enter") {
                            e.preventDefault();
                            submit();
                        } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                            e.preventDefault();
                            const step = e.key === "ArrowDown" ? 1 : -1;
                            setActive((a) => (a + step + items.length) % items.length);
                        }
                    }}
                    placeholder="Tell AI what to change…"
                    maxLength={2000}
                    aria-label="Tell AI what to change"
                    className="h-7 min-w-0 flex-1 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                />
                {instruction.trim() && (
                    <button
                        type="button"
                        onClick={submit}
                        aria-label="Change it"
                        className="inline-flex size-6 items-center justify-center rounded-md bg-sky-600 text-white transition-colors hover:bg-sky-700"
                    >
                        <ArrowUpIcon className="h-3.5 w-3.5" />
                    </button>
                )}
            </div>
            <div className="border-t border-slate-100 py-1" role="menu">
                {items.map((item, i) => (
                    <button
                        key={item.key}
                        type="button"
                        role="menuitem"
                        onMouseEnter={() => setActive(i)}
                        onClick={item.act}
                        className={`flex h-8 w-full items-center gap-2 px-2.5 text-left text-[12px] transition-colors ${
                            i === active && !instruction.trim()
                                ? item.danger
                                    ? "bg-rose-50 text-rose-700"
                                    : "bg-slate-100 text-slate-900"
                                : "text-slate-700"
                        }`}
                    >
                        {item.icon}
                        <span className="flex-1">{item.label}</span>
                        {item.hint && <Kbd combo={item.hint} variant="light" />}
                    </button>
                ))}
            </div>
            <p className="border-t border-slate-100 px-2.5 py-1.5 text-[10.5px] text-slate-400">
                {[summary, usage].filter(Boolean).join(" · ")}
            </p>
        </div>
    );
}
