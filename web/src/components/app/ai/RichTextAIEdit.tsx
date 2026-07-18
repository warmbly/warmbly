// RichTextAIEdit — "edit selection with AI" for TipTap surfaces (campaign step
// editor). Same floating pill + AIEditPopover as the textarea host; the editor
// gives us real selection coordinates via coordsAtPos. The rewrite replaces
// the selected range and stays selected for review; Undo restores a pre-edit
// HTML snapshot (the step editor runs without a history extension).

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { SparklesIcon } from "lucide-react";
import toast from "react-hot-toast";
import type { Editor } from "@tiptap/react";
import useGenerateEdit from "@/lib/api/hooks/app/generation/useGenerateEdit";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import AIEditPopover, { type AIEditPhase } from "./AIEditPopover";

interface EditorRange {
    from: number;
    to: number;
    text: string;
}

interface Anchor {
    top: number;
    bottom: number;
    centerX: number;
}

// Plain model text back to minimal TipTap HTML: paragraphs on blank lines,
// hard breaks inside them.
function plainToHTML(text: string): string {
    const esc = (s: string) =>
        s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    return text
        .split(/\n{2,}/)
        .map((p) => `<p>${esc(p).replace(/\n/g, "<br>")}</p>`)
        .join("");
}

function anchorFor(editor: Editor, from: number, to: number): Anchor | null {
    try {
        const a = editor.view.coordsAtPos(from);
        const b = editor.view.coordsAtPos(to);
        const sameLine = Math.abs(a.top - b.top) < 4;
        return {
            top: a.top,
            bottom: b.bottom,
            centerX: sameLine ? (a.left + b.right) / 2 : (a.left + a.right) / 2 + 60,
        };
    } catch {
        return null;
    }
}

export default function RichTextAIEdit({ editor }: { editor: Editor }) {
    const editMut = useGenerateEdit();
    const [range, setRange] = React.useState<EditorRange | null>(null);
    const [anchor, setAnchor] = React.useState<Anchor | null>(null);
    const [open, setOpen] = React.useState(false);
    const [phase, setPhase] = React.useState<AIEditPhase>("idle");
    const [usage, setUsage] = React.useState<{ charged: number; tokens: number } | null>(null);

    const rootRef = React.useRef<HTMLDivElement>(null);
    const frozen = React.useRef<EditorRange | null>(null);
    const lastRun = React.useRef<{
        instruction: string;
        prevHTML: string;
        range: EditorRange;
    } | null>(null);
    const openRef = React.useRef(open);
    openRef.current = open;

    const closeAll = React.useCallback(() => {
        setOpen(false);
        setPhase("idle");
        setRange(null);
        setAnchor(null);
        frozen.current = null;
    }, []);

    // Track selection while the popover is closed.
    React.useEffect(() => {
        const onSelection = () => {
            if (openRef.current) return;
            const { from, to, empty } = editor.state.selection;
            if (empty) {
                setRange(null);
                setAnchor(null);
                return;
            }
            const text = editor.state.doc.textBetween(from, to, "\n");
            setRange({ from, to, text });
            setAnchor(anchorFor(editor, from, to));
        };
        editor.on("selectionUpdate", onSelection);
        return () => {
            editor.off("selectionUpdate", onSelection);
        };
    }, [editor]);

    // Follow scrolling/resizes.
    React.useEffect(() => {
        if (!range && !frozen.current) return;
        const sync = () => {
            const target = frozen.current ?? range;
            if (!target) return;
            setAnchor(anchorFor(editor, target.from, target.to));
        };
        window.addEventListener("scroll", sync, true);
        window.addEventListener("resize", sync);
        return () => {
            window.removeEventListener("scroll", sync, true);
            window.removeEventListener("resize", sync);
        };
    }, [range, editor]);

    // Click-away and Escape while open.
    React.useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent | TouchEvent) => {
            const t = e.target as Node | null;
            if (rootRef.current?.contains(t)) return;
            if (editor.view.dom.contains(t as Node)) return;
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
    }, [open, closeAll, editor]);

    const apply = React.useCallback(
        (
            target: EditorRange,
            prevHTML: string,
            instruction: string,
            text: string,
            charged: number,
            tokens: number,
        ) => {
            setUsage({ charged, tokens });
            editor
                .chain()
                .focus()
                .insertContentAt({ from: target.from, to: target.to }, plainToHTML(text))
                .run();
            // insertContentAt leaves the caret at the end of the insertion;
            // stretch the selection back to cover it so the change is visible.
            const newTo = editor.state.selection.to;
            editor.commands.setTextSelection({ from: target.from, to: newTo });
            lastRun.current = { instruction, prevHTML, range: target };
            frozen.current = { from: target.from, to: newTo, text };
            setAnchor(anchorFor(editor, target.from, newTo));
            setPhase("applied");
        },
        [editor],
    );

    const run = React.useCallback(
        (instruction: string, target?: EditorRange, baseHTML?: string) => {
            const t = target ?? frozen.current;
            if (!t || editMut.isPending) return;
            const prevHTML = baseHTML ?? editor.getHTML();
            setPhase("busy");
            editMut.mutate(
                { text: t.text, instruction, context: editor.getText() },
                {
                    onSuccess: (res) => {
                        if (!openRef.current) return;
                        apply(
                            t,
                            prevHTML,
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
        [apply, editMut, editor],
    );

    const undo = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        editor.commands.setContent(last.prevHTML, { emitUpdate: true });
        editor.commands.setTextSelection({ from: last.range.from, to: last.range.to });
        frozen.current = last.range;
        setAnchor(anchorFor(editor, last.range.from, last.range.to));
        lastRun.current = null;
        setPhase("idle");
    }, [editor]);

    const retry = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        editor.commands.setContent(last.prevHTML, { emitUpdate: true });
        frozen.current = last.range;
        run(last.instruction, last.range, last.prevHTML);
    }, [editor, run]);

    if (typeof document === "undefined") return null;

    const showPill = !open && !!range && !!anchor && range.text.trim().length > 1;
    const vw = typeof window !== "undefined" ? window.innerWidth : 1024;
    const popAbove = (anchor?.top ?? 0) > 200;
    const popLeft = Math.min(Math.max((anchor?.centerX ?? 0) - 150, 8), vw - 308);

    return createPortal(
        <div ref={rootRef} data-floating="">
            <AnimatePresence>
                {showPill && anchor && (
                    <motion.button
                        key="ai-pill"
                        type="button"
                        initial={{ opacity: 0, y: 4, scale: 0.92, x: "-50%" }}
                        animate={{ opacity: 1, y: 0, scale: 1, x: "-50%" }}
                        exit={{ opacity: 0, y: 2, scale: 0.95, x: "-50%" }}
                        transition={{ type: "spring", stiffness: 500, damping: 32 }}
                        style={{ position: "fixed", top: anchor.top - 34, left: anchor.centerX, zIndex: 60 }}
                        className="h-7 pl-2 pr-2.5 rounded-full border border-slate-200 bg-white shadow-[0_6px_20px_-6px_rgba(15,23,42,0.25)] inline-flex items-center gap-1.5 text-[11.5px] font-medium text-slate-700 hover:text-sky-700 hover:border-sky-300 transition-colors"
                        onMouseDown={(e) => {
                            e.preventDefault();
                            if (!range) return;
                            frozen.current = range;
                            lastRun.current = null;
                            setPhase("idle");
                            setOpen(true);
                        }}
                    >
                        <SparklesIcon className="w-3 h-3 text-sky-500" />
                        Edit with AI
                    </motion.button>
                )}
                {open && anchor && (
                    <motion.div
                        key="ai-popover"
                        initial={{ opacity: 0, scale: 0.96, y: popAbove ? 4 : -4 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: popAbove ? 2 : -2 }}
                        transition={{ duration: 0.14, ease: [0.16, 1, 0.3, 1] }}
                        style={{
                            position: "fixed",
                            left: popLeft,
                            zIndex: 60,
                            ...(popAbove
                                ? { bottom: window.innerHeight - anchor.top + 8 }
                                : { top: anchor.bottom + 8 }),
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
