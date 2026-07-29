// TextareaAIEdit — inline "edit selection with AI" for plain-textarea
// composers. Select text and a small AI pill floats over the selection;
// clicking it opens the shared AIEditPopover (quick actions + free
// instruction). The rewrite types itself into the selected range, and the
// popover flips to a review row (Undo / Again / Done) with the new text left
// selected so edits can be chained.
//
// Rendered through a body portal marked data-floating, so host popovers and
// click-outside handlers treat it as part of the floating layer.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { SparklesIcon } from "lucide-react";
import toast from "react-hot-toast";
import useGenerateEdit from "@/lib/api/hooks/app/generation/useGenerateEdit";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { Kbd } from "@/components/ui/shortcut-tooltip";
import AIEditPopover, { type AIEditPhase } from "./AIEditPopover";
import textareaRangeRect, {
    textareaRangeRects,
    type LineRect,
    type RangeRect,
} from "./textareaRange";
import useTypewriter from "./useTypewriter";

interface Selection {
    start: number;
    end: number;
    text: string;
}

// Highlight tint per phase: steady while targeting, pulsing while the model
// rewrites, green-tinged once applied.
function cnHighlight(phase: AIEditPhase): string {
    if (phase === "busy") return "rounded-[3px] bg-sky-300/50 animate-pulse pointer-events-none";
    if (phase === "applied") return "rounded-[3px] bg-emerald-200/50 pointer-events-none";
    return "rounded-[3px] bg-sky-200/45 pointer-events-none";
}

interface TextareaAIEditProps {
    textareaRef: React.RefObject<HTMLTextAreaElement | null>;
    value: string;
    onChange: (next: string) => void;
    // Extra context for tone consistency (defaults to the whole value).
    getContext?: () => string;
    maxLen?: number;
}

export default function TextareaAIEdit({
    textareaRef,
    value,
    onChange,
    getContext,
    maxLen,
}: TextareaAIEditProps) {
    const editMut = useGenerateEdit();
    const typewriter = useTypewriter();

    const [sel, setSel] = React.useState<Selection | null>(null);
    const [rect, setRect] = React.useState<RangeRect | null>(null);
    // Painted selection while the popover is open: the focused popover means
    // the textarea no longer paints its own selection, so we draw it.
    const [highlights, setHighlights] = React.useState<LineRect[]>([]);
    const [open, setOpen] = React.useState(false);
    const [phase, setPhase] = React.useState<AIEditPhase>("idle");
    const [usage, setUsage] = React.useState<{ charged: number; tokens: number } | null>(null);

    const rootRef = React.useRef<HTMLDivElement>(null);
    // The selection being edited, frozen when the popover opens.
    const frozen = React.useRef<Selection | null>(null);
    // Last applied run, for Undo / Again.
    const lastRun = React.useRef<{
        instruction: string;
        prevValue: string;
        start: number;
        newLen: number;
    } | null>(null);
    // Value we last wrote ourselves; external edits while open close the UI.
    const expectedValue = React.useRef<string | null>(null);

    const openRef = React.useRef(open);
    openRef.current = open;
    const phaseRef = React.useRef(phase);
    phaseRef.current = phase;

    const closeAll = React.useCallback(() => {
        setOpen(false);
        setPhase("idle");
        setSel(null);
        setRect(null);
        setHighlights([]);
        frozen.current = null;
        expectedValue.current = null;
    }, []);

    // Re-measures the anchor rect and the painted selection for a range.
    const syncRects = React.useCallback(
        (target: Selection, paint: boolean) => {
            const ta = textareaRef.current;
            if (!ta) return;
            setRect(textareaRangeRect(ta, target.start, target.end));
            setHighlights(paint ? textareaRangeRects(ta, target.start, target.end) : []);
        },
        [textareaRef],
    );

    // ⌘J / Ctrl+J with a non-collapsed selection opens the editor directly
    // (the caret companion owns the collapsed-caret case).
    React.useEffect(() => {
        const ta = textareaRef.current;
        if (!ta) return;
        const onKey = (e: KeyboardEvent) => {
            if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "j") return;
            const { selectionStart: s, selectionEnd: en } = ta;
            if (s === en) return;
            e.preventDefault();
            const target = { start: s, end: en, text: ta.value.slice(s, en) };
            frozen.current = target;
            expectedValue.current = ta.value;
            lastRun.current = null;
            setSel(target);
            setPhase("idle");
            syncRects(target, true);
            setOpen(true);
        };
        ta.addEventListener("keydown", onKey);
        return () => ta.removeEventListener("keydown", onKey);
    }, [textareaRef, syncRects]);

    // Selection tracking: read the textarea's range whenever the document
    // selection changes while it is focused. Frozen while the popover is open.
    React.useEffect(() => {
        const onSelChange = () => {
            const ta = textareaRef.current;
            if (!ta || openRef.current) return;
            if (document.activeElement !== ta) return;
            const { selectionStart: s, selectionEnd: e } = ta;
            if (s === e) {
                setSel(null);
                setRect(null);
                return;
            }
            const next = { start: s, end: e, text: ta.value.slice(s, e) };
            setSel(next);
            setRect(textareaRangeRect(ta, s, e));
        };
        document.addEventListener("selectionchange", onSelChange);
        return () => document.removeEventListener("selectionchange", onSelChange);
    }, [textareaRef]);

    // Keep the floating layer glued to the selection through scrolling and
    // resizes (capture phase catches the textarea's own scroll too).
    React.useEffect(() => {
        if (!sel && !frozen.current) return;
        const sync = () => {
            const target = frozen.current ?? sel;
            if (!target) return;
            syncRects(target, openRef.current);
        };
        window.addEventListener("scroll", sync, true);
        window.addEventListener("resize", sync);
        return () => {
            window.removeEventListener("scroll", sync, true);
            window.removeEventListener("resize", sync);
        };
    }, [sel, syncRects]);

    // Dismiss when the user clicks anywhere outside the floating layer and
    // the textarea (mirrors useClickOutside, plus the textarea exception).
    React.useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent | TouchEvent) => {
            const t = e.target as Node | null;
            if (rootRef.current?.contains(t)) return;
            if (textareaRef.current?.contains(t)) return;
            closeAll();
        };
        document.addEventListener("mousedown", onDown, true);
        document.addEventListener("touchstart", onDown, true);
        return () => {
            document.removeEventListener("mousedown", onDown, true);
            document.removeEventListener("touchstart", onDown, true);
        };
    }, [open, closeAll, textareaRef]);

    // External edits (typing, template insert, discard) while open: bail out
    // rather than rewriting stale ranges.
    React.useEffect(() => {
        if (!open) return;
        if (expectedValue.current !== null && value !== expectedValue.current) closeAll();
    }, [value, open, closeAll]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.stopPropagation();
                closeAll();
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, [open, closeAll]);

    const applyResult = React.useCallback(
        (
            target: Selection,
            prevValue: string,
            instruction: string,
            text: string,
            charged: number,
            tokens: number,
        ) => {
            setUsage({ charged, tokens });
            const prefix = prevValue.slice(0, target.start);
            const suffix = prevValue.slice(target.end);
            const cap = (s: string) => (maxLen ? s.slice(0, maxLen) : s);
            typewriter.run(
                text,
                (partial) => {
                    const next = cap(prefix + partial + suffix);
                    expectedValue.current = next;
                    onChange(next);
                    // Grow the painted highlight with the typing rewrite.
                    const ta = textareaRef.current;
                    if (ta) {
                        setHighlights(
                            textareaRangeRects(ta, target.start, target.start + partial.length),
                        );
                    }
                },
                () => {
                    const newRange = { start: target.start, end: target.start + text.length, text };
                    lastRun.current = { instruction, prevValue, start: target.start, newLen: text.length };
                    frozen.current = newRange;
                    const ta = textareaRef.current;
                    if (ta) {
                        // Leave the rewrite selected so it reads as "this changed"
                        // and a follow-up edit can chain on it.
                        ta.setSelectionRange(newRange.start, newRange.end);
                    }
                    syncRects(newRange, true);
                    setPhase("applied");
                },
            );
        },
        [maxLen, onChange, syncRects, textareaRef, typewriter],
    );

    const run = React.useCallback(
        (instruction: string, target?: Selection, baseValue?: string) => {
            const t = target ?? frozen.current;
            if (!t || editMut.isPending) return;
            const prevValue = baseValue ?? value;
            setPhase("busy");
            editMut.mutate(
                {
                    text: t.text,
                    instruction,
                    context: getContext?.() ?? prevValue,
                },
                {
                    onSuccess: (res) => {
                        if (!openRef.current) return;
                        applyResult(
                            t,
                            prevValue,
                            instruction,
                            res.text,
                            res.credits_charged ?? 0,
                            res.tokens_used ?? 0,
                        );
                    },
                    onError: (e) => {
                        const err = e as unknown as AppError;
                        if (err?.status === 402) {
                            toast.error("You're out of AI credits. Upgrade or purchase more to keep editing with AI.");
                        } else {
                            toast.error(buildError(err));
                        }
                        setPhase("idle");
                    },
                },
            );
        },
        [applyResult, editMut, getContext, value],
    );

    const undo = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        typewriter.cancel();
        expectedValue.current = last.prevValue;
        onChange(last.prevValue);
        const ta = textareaRef.current;
        const origEnd = last.prevValue.length - (value.length - (last.start + last.newLen));
        const restored = {
            start: last.start,
            end: origEnd,
            text: last.prevValue.slice(last.start, origEnd),
        };
        frozen.current = restored;
        if (ta) ta.setSelectionRange(last.start, origEnd);
        syncRects(restored, true);
        lastRun.current = null;
        setPhase("idle");
    }, [onChange, syncRects, textareaRef, typewriter, value]);

    const retry = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        const origEnd = last.prevValue.length - (value.length - (last.start + last.newLen));
        const target = {
            start: last.start,
            end: origEnd,
            text: last.prevValue.slice(last.start, origEnd),
        };
        frozen.current = target;
        run(last.instruction, target, last.prevValue);
    }, [run, value]);

    const openEditor = () => {
        if (!sel) return;
        frozen.current = sel;
        expectedValue.current = value;
        lastRun.current = null;
        setPhase("idle");
        syncRects(sel, true);
        setOpen(true);
    };

    if (typeof document === "undefined") return null;

    const showPill = !open && !!sel && !!rect && sel.text.trim().length > 1;

    // Popover placement: above the selection when there is room, else below.
    const vw = typeof window !== "undefined" ? window.innerWidth : 1024;
    const popAbove = (rect?.top ?? 0) > 200;
    const popLeft = Math.min(Math.max((rect?.centerX ?? 0) - 150, 8), vw - 308);

    return createPortal(
        <div ref={rootRef} data-floating="">
            {/* Painted selection: the range being edited, pulsing while the
                rewrite is generating and growing as it types in. */}
            {open &&
                highlights.map((h, i) => (
                    <div
                        key={i}
                        aria-hidden
                        className={cnHighlight(phase)}
                        style={{
                            position: "fixed",
                            top: h.top,
                            left: h.left,
                            width: h.width,
                            height: h.height,
                            zIndex: 114,
                        }}
                    />
                ))}
            <AnimatePresence>
                {showPill && rect && (
                    <motion.button
                        key="ai-pill"
                        type="button"
                        initial={{ opacity: 0, y: 4, scale: 0.92, x: "-50%" }}
                        animate={{ opacity: 1, y: 0, scale: 1, x: "-50%" }}
                        exit={{ opacity: 0, y: 2, scale: 0.95, x: "-50%" }}
                        transition={{ type: "spring", stiffness: 500, damping: 32 }}
                        style={{ position: "fixed", top: rect.top - 34, left: rect.centerX, zIndex: 120 }}
                        className="h-7 pl-2 pr-2.5 rounded-full border border-slate-200 bg-white shadow-[0_6px_20px_-6px_rgba(15,23,42,0.25)] inline-flex items-center gap-1.5 text-[11.5px] font-medium text-slate-700 hover:text-sky-700 hover:border-sky-300 transition-colors"
                        onMouseDown={(e) => {
                            // Keep the textarea focused so the selection survives.
                            e.preventDefault();
                            openEditor();
                        }}
                    >
                        <SparklesIcon className="w-3 h-3 text-sky-500" />
                        Edit with AI
                        <Kbd combo="mod+J" variant="light" />
                    </motion.button>
                )}
                {open && rect && (
                    <motion.div
                        key="ai-popover"
                        initial={{ opacity: 0, scale: 0.96, y: popAbove ? 4 : -4 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: popAbove ? 2 : -2 }}
                        transition={{ duration: 0.14, ease: [0.16, 1, 0.3, 1] }}
                        style={{
                            position: "fixed",
                            left: popLeft,
                            zIndex: 120,
                            // Anchored via bottom when flipping above, so no
                            // translate is needed (motion owns transform).
                            ...(popAbove
                                ? { bottom: window.innerHeight - rect.top + 8 }
                                : { top: rect.bottom + 8 }),
                        }}
                        className="rounded-lg border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.22)] overflow-hidden"
                    >
                        <AIEditPopover
                            phase={phase}
                            usage={usage}
                            onRun={(instruction) => run(instruction)}
                            onUndo={undo}
                            onRetry={retry}
                            onDone={closeAll}
                        />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>,
        document.body,
    );
}
