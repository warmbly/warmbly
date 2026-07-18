// TextareaAICaret — the AI entry point that lives where you type, not in the
// action bar. While the textarea is focused with no selection, a faint sparkle
// sits in the right gutter aligned with the caret's line; clicking it (or ⌘J)
// opens a small menu AT the caret: a free "ask AI to write" instruction,
// drafting a full reply from the thread, or continuing the draft. Generated
// text types itself in at the caret and can be undone or regenerated.
//
// Complements TextareaAIEdit (which owns the non-collapsed-selection pill);
// the two are naturally exclusive: this renders only for a collapsed caret.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowUpIcon,
    CheckIcon,
    CornerUpLeftIcon,
    PenLineIcon,
    RefreshCwIcon,
    SparklesIcon,
    Undo2Icon,
} from "lucide-react";
import toast from "react-hot-toast";
import useGenerateWrite from "@/lib/api/hooks/app/generation/useGenerateWrite";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import textareaRangeRect, { type RangeRect } from "./textareaRange";
import useTypewriter from "./useTypewriter";

// Context window around the caret sent with freeform prompts, so long drafts
// don't blow past the endpoint's prompt cap.
const CONTEXT_WINDOW = 1500;

interface TextareaAICaretProps {
    textareaRef: React.RefObject<HTMLTextAreaElement | null>;
    value: string;
    onChange: (next: string) => void;
    // Kicks off the host's thread-grounded draft flow (the draft bar).
    onDraftReply?: () => void;
    // Extra context line for freeform prompts (e.g. the subject).
    contextHint?: string;
    maxLen?: number;
}

function buildInsertPrompt(
    instruction: string,
    value: string,
    caret: number,
    contextHint?: string,
): string {
    const before = value.slice(Math.max(0, caret - CONTEXT_WINDOW), caret);
    const after = value.slice(caret, caret + Math.floor(CONTEXT_WINDOW / 3));
    let p =
        instruction +
        "\n\nYou are writing text to insert into an email draft at the cursor. Return ONLY the text to insert: no preamble, no subject line, no signature, no quotes around it. Match the draft's language and tone.";
    if (contextHint) p += `\n\n${contextHint}`;
    if (before.trim() || after.trim()) {
        p += `\n\nDraft so far, cursor marked with [[CURSOR]]:\n${before}[[CURSOR]]${after}`;
    }
    return p;
}

type Phase = "idle" | "busy" | "applied";

export default function TextareaAICaret({
    textareaRef,
    value,
    onChange,
    onDraftReply,
    contextHint,
    maxLen,
}: TextareaAICaretProps) {
    const writeMut = useGenerateWrite();
    const typewriter = useTypewriter();

    const [caret, setCaret] = React.useState<number | null>(null);
    const [rect, setRect] = React.useState<RangeRect | null>(null);
    const [taRight, setTaRight] = React.useState(0);
    const [open, setOpen] = React.useState(false);
    const [phase, setPhase] = React.useState<Phase>("idle");
    const [credits, setCredits] = React.useState<number | null>(null);
    const [instruction, setInstruction] = React.useState("");

    const rootRef = React.useRef<HTMLDivElement>(null);
    const inputRef = React.useRef<HTMLInputElement>(null);
    const frozenCaret = React.useRef<number | null>(null);
    const lastRun = React.useRef<{
        instruction: string;
        prevValue: string;
        at: number;
        newLen: number;
    } | null>(null);
    const expectedValue = React.useRef<string | null>(null);
    const openRef = React.useRef(open);
    openRef.current = open;

    const closeAll = React.useCallback(() => {
        setOpen(false);
        setPhase("idle");
        setInstruction("");
        frozenCaret.current = null;
        expectedValue.current = null;
    }, []);

    const measure = React.useCallback(
        (pos: number) => {
            const ta = textareaRef.current;
            if (!ta) return;
            setRect(textareaRangeRect(ta, pos, pos));
            setTaRight(ta.getBoundingClientRect().right);
        },
        [textareaRef],
    );

    // Follow the caret while the menu is closed.
    React.useEffect(() => {
        const onSelChange = () => {
            const ta = textareaRef.current;
            if (!ta || openRef.current) return;
            if (document.activeElement !== ta) {
                setCaret(null);
                setRect(null);
                return;
            }
            const { selectionStart: s, selectionEnd: e } = ta;
            if (s !== e) {
                // Non-collapsed selection belongs to the edit pill.
                setCaret(null);
                setRect(null);
                return;
            }
            setCaret(s);
            measure(s);
        };
        document.addEventListener("selectionchange", onSelChange);
        return () => document.removeEventListener("selectionchange", onSelChange);
    }, [textareaRef, measure]);

    // Blur hides the companion (unless the menu is open).
    React.useEffect(() => {
        const ta = textareaRef.current;
        if (!ta) return;
        const onBlur = () => {
            if (openRef.current) return;
            setCaret(null);
            setRect(null);
        };
        ta.addEventListener("blur", onBlur);
        return () => ta.removeEventListener("blur", onBlur);
    }, [textareaRef]);

    // ⌘J / Ctrl+J opens the menu at the caret.
    React.useEffect(() => {
        const ta = textareaRef.current;
        if (!ta) return;
        const onKey = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "j") {
                e.preventDefault();
                const pos = ta.selectionStart ?? ta.value.length;
                frozenCaret.current = pos;
                expectedValue.current = ta.value;
                lastRun.current = null;
                setPhase("idle");
                measure(pos);
                setOpen(true);
            }
        };
        ta.addEventListener("keydown", onKey);
        return () => ta.removeEventListener("keydown", onKey);
    }, [textareaRef, measure]);

    // Keep the layer glued through scroll/resize.
    React.useEffect(() => {
        if (caret === null && frozenCaret.current === null) return;
        const sync = () => {
            const pos = frozenCaret.current ?? caret;
            if (pos !== null) measure(pos);
        };
        window.addEventListener("scroll", sync, true);
        window.addEventListener("resize", sync);
        return () => {
            window.removeEventListener("scroll", sync, true);
            window.removeEventListener("resize", sync);
        };
    }, [caret, measure]);

    // Click-away and Escape while open.
    React.useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent | TouchEvent) => {
            const t = e.target as Node | null;
            if (rootRef.current?.contains(t)) return;
            closeAll();
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.stopPropagation();
                closeAll();
            }
        };
        document.addEventListener("mousedown", onDown, true);
        document.addEventListener("touchstart", onDown, true);
        document.addEventListener("keydown", onKey, true);
        return () => {
            document.removeEventListener("mousedown", onDown, true);
            document.removeEventListener("touchstart", onDown, true);
            document.removeEventListener("keydown", onKey, true);
        };
    }, [open, closeAll]);

    // External edits while open (typing, draft bar) close the menu.
    React.useEffect(() => {
        if (!open) return;
        if (expectedValue.current !== null && value !== expectedValue.current) closeAll();
    }, [value, open, closeAll]);

    React.useEffect(() => {
        if (open && phase === "idle") inputRef.current?.focus();
    }, [open, phase]);

    const openMenu = () => {
        const ta = textareaRef.current;
        const pos = frozenCaret.current ?? caret ?? ta?.selectionStart ?? 0;
        frozenCaret.current = pos;
        expectedValue.current = value;
        lastRun.current = null;
        setPhase("idle");
        measure(pos);
        setOpen(true);
    };

    const run = React.useCallback(
        (rawInstruction: string, baseValue?: string, at?: number) => {
            const pos = at ?? frozenCaret.current;
            if (pos === null || pos === undefined || writeMut.isPending) return;
            const prevValue = baseValue ?? value;
            setPhase("busy");
            writeMut.mutate(
                { prompt: buildInsertPrompt(rawInstruction, prevValue, pos, contextHint) },
                {
                    onSuccess: (res) => {
                        if (!openRef.current) return;
                        setCredits(res.credits_remaining);
                        const before = prevValue.slice(0, pos);
                        // Breathing room around the insertion without doubling
                        // whitespace that is already there.
                        const lead = before && !/\s$/.test(before) ? " " : "";
                        const after = prevValue.slice(pos);
                        const cap = (s: string) => (maxLen ? s.slice(0, maxLen) : s);
                        typewriter.run(
                            res.text,
                            (partial) => {
                                const next = cap(before + lead + partial + after);
                                expectedValue.current = next;
                                onChange(next);
                            },
                            () => {
                                const newLen = lead.length + res.text.length;
                                lastRun.current = {
                                    instruction: rawInstruction,
                                    prevValue,
                                    at: pos,
                                    newLen,
                                };
                                frozenCaret.current = pos + newLen;
                                measure(pos + newLen);
                                setPhase("applied");
                            },
                        );
                    },
                    onError: (e) => {
                        const err = e as unknown as AppError;
                        if (err?.status === 402) {
                            toast.error("You're out of AI credits. Upgrade or purchase more to keep writing with AI.");
                        } else {
                            toast.error(buildError(err));
                        }
                        setPhase("idle");
                    },
                },
            );
        },
        [contextHint, maxLen, measure, onChange, typewriter, value, writeMut],
    );

    const undo = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        typewriter.cancel();
        expectedValue.current = last.prevValue;
        onChange(last.prevValue);
        frozenCaret.current = last.at;
        measure(last.at);
        lastRun.current = null;
        setPhase("idle");
    }, [measure, onChange, typewriter]);

    const retry = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        run(last.instruction, last.prevValue, last.at);
    }, [run]);

    const submit = () => {
        const text = instruction.trim();
        if (!text) return;
        run(text);
    };

    if (typeof document === "undefined") return null;

    const showCompanion = !open && caret !== null && !!rect;
    const vw = typeof window !== "undefined" ? window.innerWidth : 1024;
    const popAbove = (rect?.top ?? 0) > 240;
    const popLeft = Math.min(Math.max((rect?.left ?? 0) - 20, 8), vw - 328);

    return createPortal(
        <div ref={rootRef} data-floating="">
            <AnimatePresence>
                {showCompanion && rect && (
                    <motion.button
                        key="ai-caret-companion"
                        type="button"
                        initial={{ opacity: 0, scale: 0.8 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.9 }}
                        transition={{ duration: 0.12 }}
                        style={{
                            position: "fixed",
                            top: rect.top - 1,
                            left: taRight - 30,
                            zIndex: 55,
                        }}
                        title="Write with AI (⌘J)"
                        aria-label="Write with AI"
                        className="size-[22px] rounded-md inline-flex items-center justify-center text-slate-300 hover:text-sky-600 hover:bg-sky-50 transition-colors"
                        onMouseDown={(e) => {
                            // Keep the textarea focused and the caret in place.
                            e.preventDefault();
                            openMenu();
                        }}
                    >
                        <SparklesIcon className="w-3.5 h-3.5" />
                    </motion.button>
                )}
                {open && rect && (
                    <motion.div
                        key="ai-caret-menu"
                        initial={{ opacity: 0, scale: 0.96, y: popAbove ? 4 : -4 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: popAbove ? 2 : -2 }}
                        transition={{ duration: 0.14, ease: [0.16, 1, 0.3, 1] }}
                        style={{
                            position: "fixed",
                            left: popLeft,
                            zIndex: 60,
                            ...(popAbove
                                ? { bottom: window.innerHeight - rect.top + 6 }
                                : { top: rect.bottom + 6 }),
                        }}
                        className="w-80 rounded-lg border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.22)] overflow-hidden"
                    >
                        {phase === "busy" ? (
                            <div className="px-3 py-2.5 flex items-center gap-2">
                                <SparklesIcon className="w-3.5 h-3.5 text-sky-500 animate-pulse shrink-0" />
                                <span className="ai-shimmer-text text-[12px] font-medium">Writing…</span>
                            </div>
                        ) : phase === "applied" ? (
                            <div className="px-2.5 py-2 flex items-center gap-1.5">
                                <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-slate-900 mr-auto">
                                    <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                                    Inserted
                                    {credits !== null && (
                                        <span className="text-[10.5px] font-normal text-slate-400">
                                            · {credits} credit{credits === 1 ? "" : "s"} left
                                        </span>
                                    )}
                                </span>
                                <button
                                    type="button"
                                    onClick={undo}
                                    className="h-6 px-1.5 rounded inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                >
                                    <Undo2Icon className="w-3 h-3" />
                                    Undo
                                </button>
                                <button
                                    type="button"
                                    onClick={retry}
                                    className="h-6 px-1.5 rounded inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                >
                                    <RefreshCwIcon className="w-3 h-3" />
                                    Again
                                </button>
                                <button
                                    type="button"
                                    onClick={closeAll}
                                    className="h-6 px-2 rounded bg-slate-900 text-white text-[11.5px] font-medium hover:bg-slate-700 transition-colors"
                                >
                                    Done
                                </button>
                            </div>
                        ) : (
                            <div>
                                <div className="flex items-center gap-1.5 px-2.5 pt-2.5 pb-2">
                                    <SparklesIcon className="w-3.5 h-3.5 text-sky-500 shrink-0" />
                                    <input
                                        ref={inputRef}
                                        value={instruction}
                                        onChange={(e) => setInstruction(e.target.value)}
                                        onKeyDown={(e) => {
                                            if (e.key === "Enter") {
                                                e.preventDefault();
                                                submit();
                                            }
                                        }}
                                        placeholder="Ask AI to write…"
                                        maxLength={2000}
                                        className="flex-1 min-w-0 h-7 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                                    />
                                    <button
                                        type="button"
                                        onClick={submit}
                                        disabled={!instruction.trim()}
                                        aria-label="Write at the cursor"
                                        className="size-6 rounded-md bg-sky-600 text-white inline-flex items-center justify-center hover:bg-sky-700 transition-colors disabled:opacity-40"
                                    >
                                        <ArrowUpIcon className="w-3.5 h-3.5" />
                                    </button>
                                </div>
                                <div className="border-t border-slate-100 py-1">
                                    {onDraftReply && (
                                        <button
                                            type="button"
                                            onClick={() => {
                                                closeAll();
                                                onDraftReply();
                                            }}
                                            className="w-full px-2.5 h-8 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-50 transition-colors"
                                        >
                                            <CornerUpLeftIcon className="w-3.5 h-3.5 text-slate-400" />
                                            Draft a reply from this thread
                                            <span className="ml-auto text-[10px] text-slate-300">2 credits</span>
                                        </button>
                                    )}
                                    <button
                                        type="button"
                                        onClick={() =>
                                            run(
                                                "Continue the draft naturally from the cursor, adding the next sentence or two.",
                                            )
                                        }
                                        disabled={!value.trim()}
                                        className="w-full px-2.5 h-8 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-50 transition-colors disabled:opacity-40"
                                    >
                                        <PenLineIcon className="w-3.5 h-3.5 text-slate-400" />
                                        Continue writing
                                        <span className="ml-auto text-[10px] text-slate-300">1 credit</span>
                                    </button>
                                </div>
                            </div>
                        )}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>,
        document.body,
    );
}
