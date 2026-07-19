// AIDraftBar — makes AI drafting feel native to the composer instead of a
// fire-and-forget toast. Renders as a floating overlay INSIDE the body area
// (no layout shift): while generating, a pill with a staged shimmer status and
// a cancel; once the draft has typed itself in, a floating review row — Keep,
// Adjust (steer with an instruction and regenerate), Retry, or Discard
// (restores what was there before) — plus what the call actually cost from
// the usage-based settle.
//
// useAIDraft owns the state machine and is generator-agnostic: the host passes
// an async generate(instruction?) so the same bar can drive any composer.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowUpIcon,
    CheckIcon,
    MessageCircleQuestionIcon,
    RefreshCwIcon,
    SlidersHorizontalIcon,
    SparklesIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useTypewriter from "./useTypewriter";
import formatUsage from "./usage";

export interface AIDraftController {
    phase: "idle" | "busy" | "review" | "question";
    // What the last draft actually cost (usage-based settle), when metered.
    usage: { charged: number; tokens: number } | null;
    // Clarifying question the generator asked instead of drafting (grounded
    // endpoints ask when they can't tell what the email is for).
    question: string | null;
    // What the last draft was grounded in, when the endpoint reports it.
    grounding: AIDraftGrounding | null;
    start: (instruction?: string) => void;
    keep: () => void;
    discard: () => void;
    regenerate: () => void;
    adjust: (instruction: string) => void;
    // Answer the pending clarifying question and generate for real.
    answer: (text: string) => void;
    cancel: () => void;
}

export interface AIDraftGrounding {
    contact: boolean;
    history: number;
    voice_profile: boolean;
}

interface GenerateResult {
    // Absent when the generator asked a clarifying question instead.
    text?: string;
    question?: string;
    grounding?: AIDraftGrounding;
    credits_remaining: number;
    credits_charged?: number;
    tokens_used?: number;
}

interface UseAIDraftOptions {
    value: string;
    onChange: (next: string) => void;
    generate: (instruction?: string) => Promise<GenerateResult>;
    maxLen?: number;
}

export function useAIDraft({ value, onChange, generate, maxLen }: UseAIDraftOptions): AIDraftController {
    const typewriter = useTypewriter();
    const [phase, setPhase] = React.useState<"idle" | "busy" | "review" | "question">("idle");
    const [usage, setUsage] = React.useState<{ charged: number; tokens: number } | null>(null);
    const [question, setQuestion] = React.useState<string | null>(null);
    const [grounding, setGrounding] = React.useState<AIDraftGrounding | null>(null);

    // Latest body without re-binding callbacks every keystroke.
    const valueRef = React.useRef(value);
    valueRef.current = value;
    // Body as it was before the current draft, for Discard / Regenerate.
    const prevBody = React.useRef("");
    // The instruction behind the current run, so a question-answer can fold
    // into it on the follow-up generation.
    const lastInstruction = React.useRef<string | undefined>(undefined);
    // Bumped to invalidate in-flight generations on cancel/unmount.
    const runId = React.useRef(0);

    const cap = React.useCallback(
        (s: string) => (maxLen ? s.slice(0, maxLen) : s),
        [maxLen],
    );

    const runGeneration = React.useCallback(
        (instruction: string | undefined, base: string) => {
            const id = ++runId.current;
            lastInstruction.current = instruction;
            setPhase("busy");
            generate(instruction)
                .then((res) => {
                    if (runId.current !== id) return;
                    setUsage({
                        charged: res.credits_charged ?? 0,
                        tokens: res.tokens_used ?? 0,
                    });
                    setGrounding(res.grounding ?? null);
                    if (res.question) {
                        // The generator wants more context before writing.
                        setQuestion(res.question);
                        setPhase("question");
                        return;
                    }
                    const prefix = base.trim() ? `${base.trimEnd()}\n\n` : "";
                    typewriter.run(
                        res.text ?? "",
                        (partial) => onChange(cap(prefix + partial)),
                        () => setPhase("review"),
                    );
                })
                .catch((e) => {
                    if (runId.current !== id) return;
                    const err = e as AppError;
                    if (err?.status === 402) {
                        toast.error("You're out of AI credits. Upgrade or purchase more to keep drafting.");
                    } else {
                        toast.error(buildError(err));
                    }
                    setPhase("idle");
                });
        },
        [cap, generate, onChange, typewriter],
    );

    const start = React.useCallback(
        (instruction?: string) => {
            if (phase === "busy") return;
            prevBody.current = valueRef.current;
            runGeneration(instruction, valueRef.current);
        },
        [phase, runGeneration],
    );

    const keep = React.useCallback(() => {
        setGrounding(null);
        setPhase("idle");
    }, []);

    const discard = React.useCallback(() => {
        typewriter.cancel();
        onChange(prevBody.current);
        setGrounding(null);
        setPhase("idle");
    }, [onChange, typewriter]);

    const regenerate = React.useCallback(() => {
        typewriter.cancel();
        onChange(prevBody.current);
        runGeneration(undefined, prevBody.current);
    }, [onChange, runGeneration, typewriter]);

    const adjust = React.useCallback(
        (instruction: string) => {
            typewriter.cancel();
            onChange(prevBody.current);
            runGeneration(instruction, prevBody.current);
        },
        [onChange, runGeneration, typewriter],
    );

    const answer = React.useCallback(
        (text: string) => {
            const q = question;
            setQuestion(null);
            const combined = [
                lastInstruction.current?.trim(),
                q ? `You asked: "${q}" The user answered: ${text}` : text,
            ]
                .filter(Boolean)
                .join("\n");
            runGeneration(combined, prevBody.current);
        },
        [question, runGeneration],
    );

    const cancel = React.useCallback(() => {
        runId.current++;
        typewriter.cancel();
        onChange(prevBody.current);
        setQuestion(null);
        setGrounding(null);
        setPhase("idle");
    }, [onChange, typewriter]);

    return { phase, usage, question, grounding, start, keep, discard, regenerate, adjust, answer, cancel };
}

export default function AIDraftBar({
    ctrl,
    busyLabels,
}: {
    ctrl: AIDraftController;
    // Staged status labels shown while generating, advancing every ~1.5s.
    busyLabels?: string[];
}) {
    const labels = React.useMemo(
        () => (busyLabels?.length ? busyLabels : ["Writing…"]),
        [busyLabels],
    );
    const [stage, setStage] = React.useState(0);
    const [adjustOpen, setAdjustOpen] = React.useState(false);
    const [instruction, setInstruction] = React.useState("");

    React.useEffect(() => {
        if (ctrl.phase !== "busy") {
            setStage(0);
            return;
        }
        const t = setInterval(
            () => setStage((s) => Math.min(s + 1, labels.length - 1)),
            1500,
        );
        return () => clearInterval(t);
    }, [ctrl.phase, labels.length]);

    React.useEffect(() => {
        if (ctrl.phase !== "review") {
            setAdjustOpen(false);
            setInstruction("");
        }
    }, [ctrl.phase]);

    const submitAdjust = () => {
        const text = instruction.trim();
        if (!text) return;
        ctrl.adjust(text);
    };

    const usageText = ctrl.usage ? formatUsage(ctrl.usage.charged, ctrl.usage.tokens) : "";

    // Floats over the bottom of the body area; the wrapper is pointer-inert so
    // the textarea stays clickable around the card.
    return (
        <div className="pointer-events-none absolute inset-x-0 bottom-2 z-10 flex justify-center px-3">
            <AnimatePresence initial={false}>
                {ctrl.phase === "busy" && (
                    <motion.div
                        key="ai-draft-busy"
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 6, scale: 0.97 }}
                        transition={{ type: "spring", stiffness: 480, damping: 34 }}
                        className="pointer-events-auto h-8 pl-3 pr-1.5 rounded-full border border-slate-200 bg-white/95 backdrop-blur shadow-[0_8px_24px_-8px_rgba(15,23,42,0.25)] flex items-center gap-2"
                    >
                        <SparklesIcon className="w-3.5 h-3.5 text-sky-500 animate-pulse shrink-0" />
                        <AnimatePresence mode="wait" initial={false}>
                            <motion.span
                                key={stage}
                                initial={{ opacity: 0, y: 3 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: -3 }}
                                transition={{ duration: 0.15 }}
                                className="ai-shimmer-text text-[12px] font-medium"
                            >
                                {labels[stage]}
                            </motion.span>
                        </AnimatePresence>
                        <button
                            type="button"
                            onClick={ctrl.cancel}
                            aria-label="Cancel draft"
                            className="size-6 rounded-full inline-flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </motion.div>
                )}
                {ctrl.phase === "question" && (
                    <motion.div
                        key="ai-draft-question"
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 6, scale: 0.97 }}
                        transition={{ type: "spring", stiffness: 480, damping: 34 }}
                        className="pointer-events-auto w-[400px] max-w-[92vw] rounded-lg border border-slate-200 bg-white/95 backdrop-blur shadow-[0_8px_24px_-8px_rgba(15,23,42,0.25)] overflow-hidden"
                    >
                        <div className="px-3 pt-2.5 flex items-start gap-2">
                            <MessageCircleQuestionIcon className="w-3.5 h-3.5 text-sky-500 shrink-0 mt-0.5" />
                            <p className="text-[12px] text-slate-800 leading-snug">
                                {ctrl.question}
                            </p>
                            <button
                                type="button"
                                onClick={ctrl.cancel}
                                aria-label="Dismiss question"
                                className="ml-auto size-5 shrink-0 rounded inline-flex items-center justify-center text-slate-300 hover:text-slate-600 hover:bg-slate-100 transition-colors"
                            >
                                <XIcon className="w-3 h-3" />
                            </button>
                        </div>
                        <QuestionAnswerRow onAnswer={ctrl.answer} />
                    </motion.div>
                )}
                {ctrl.phase === "review" && (
                    <motion.div
                        key="ai-draft-review"
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 6, scale: 0.97 }}
                        transition={{ type: "spring", stiffness: 480, damping: 34 }}
                        className="pointer-events-auto w-[400px] max-w-[92vw] rounded-lg border border-slate-200 bg-white/95 backdrop-blur shadow-[0_8px_24px_-8px_rgba(15,23,42,0.25)] overflow-hidden"
                    >
                        <div className="px-3 pt-2 flex items-center gap-1.5">
                            <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-slate-900">
                                <SparklesIcon className="w-3.5 h-3.5 text-sky-500" />
                                Draft ready
                            </span>
                            {usageText && (
                                <span className="ml-auto text-[10.5px] text-slate-400">
                                    {usageText}
                                </span>
                            )}
                        </div>
                        {ctrl.grounding && (
                            <div className="px-3 pt-1 text-[10px] text-slate-400">
                                Grounded in{" "}
                                {[
                                    ctrl.grounding.contact ? "the contact's profile" : null,
                                    ctrl.grounding.history > 0
                                        ? `${ctrl.grounding.history} past email${ctrl.grounding.history === 1 ? "" : "s"}`
                                        : null,
                                    ctrl.grounding.voice_profile ? "your voice profile" : null,
                                ]
                                    .filter(Boolean)
                                    .join(" · ") || "the recipient address only"}
                            </div>
                        )}
                        <div className="px-2 py-2 flex items-center justify-end gap-1">
                            <button
                                type="button"
                                onClick={() => setAdjustOpen((o) => !o)}
                                className={`h-[26px] px-2 rounded-md inline-flex items-center gap-1 text-[11.5px] transition-colors ${
                                    adjustOpen
                                        ? "text-sky-700 bg-sky-50"
                                        : "text-slate-600 hover:text-slate-900 hover:bg-slate-100"
                                }`}
                            >
                                <SlidersHorizontalIcon className="w-3 h-3" />
                                Adjust
                            </button>
                            <button
                                type="button"
                                onClick={ctrl.regenerate}
                                className="h-[26px] px-2 rounded-md inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                <RefreshCwIcon className="w-3 h-3" />
                                Retry
                            </button>
                            <button
                                type="button"
                                onClick={ctrl.discard}
                                className="h-[26px] px-2 rounded-md inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-rose-700 hover:bg-rose-50 transition-colors"
                            >
                                <Trash2Icon className="w-3 h-3" />
                                Discard
                            </button>
                            <button
                                type="button"
                                onClick={ctrl.keep}
                                className="h-[26px] px-2.5 rounded-md bg-slate-900 text-white text-[11.5px] font-medium inline-flex items-center gap-1 hover:bg-slate-700 transition-colors"
                            >
                                <CheckIcon className="w-3 h-3" />
                                Keep
                            </button>
                        </div>
                        <AnimatePresence initial={false}>
                            {adjustOpen && (
                                <motion.div
                                    key="adjust"
                                    initial={{ opacity: 0, height: 0 }}
                                    animate={{ opacity: 1, height: "auto" }}
                                    exit={{ opacity: 0, height: 0 }}
                                    transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
                                    className="overflow-hidden border-t border-slate-100"
                                >
                                    <div className="px-2 py-1.5 flex items-center gap-1.5 w-full">
                                        <input
                                            autoFocus
                                            value={instruction}
                                            onChange={(e) => setInstruction(e.target.value)}
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") {
                                                    e.preventDefault();
                                                    submitAdjust();
                                                }
                                            }}
                                            placeholder="e.g. shorter, mention the pricing page, ask for Tuesday"
                                            maxLength={1000}
                                            className="flex-1 min-w-0 h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                        />
                                        <button
                                            type="button"
                                            onClick={submitAdjust}
                                            disabled={!instruction.trim()}
                                            aria-label="Redraft with this instruction"
                                            className="size-7 rounded-md bg-sky-600 text-white inline-flex items-center justify-center hover:bg-sky-700 transition-colors disabled:opacity-40"
                                        >
                                            <ArrowUpIcon className="w-3.5 h-3.5" />
                                        </button>
                                    </div>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// QuestionAnswerRow — the reply input under a clarifying question. Enter (or
// the arrow) answers and regenerates with the answer folded in.
function QuestionAnswerRow({ onAnswer }: { onAnswer: (text: string) => void }) {
    const [text, setText] = React.useState("");
    const submit = () => {
        const t = text.trim();
        if (!t) return;
        onAnswer(t);
    };
    return (
        <div className="px-2 py-1.5 flex items-center gap-1.5">
            <input
                autoFocus
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === "Enter") {
                        e.preventDefault();
                        submit();
                    }
                }}
                placeholder="Answer, then it writes the email"
                maxLength={1000}
                className="flex-1 min-w-0 h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
            />
            <button
                type="button"
                onClick={submit}
                disabled={!text.trim()}
                aria-label="Answer and draft"
                className="size-7 rounded-md bg-sky-600 text-white inline-flex items-center justify-center hover:bg-sky-700 transition-colors disabled:opacity-40"
            >
                <ArrowUpIcon className="w-3.5 h-3.5" />
            </button>
        </div>
    );
}
