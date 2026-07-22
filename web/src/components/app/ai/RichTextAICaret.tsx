// RichTextAICaret — the caret AI assistant for the campaign body (TipTap),
// mirroring the composer's TextareaAICaret so writing with AI feels identical
// everywhere. While the editor is focused with a collapsed caret, a faint sparkle
// sits in the right gutter aligned with the caret line; clicking it (or ⌘J) opens
// a small menu AT the caret to ask AI to write, or continue the draft. Generated
// text is inserted at the caret and can be undone or regenerated.
//
// Complements RichTextAIEdit (the non-collapsed-selection "Edit with AI" pill);
// the two are exclusive (this only renders for a collapsed caret).

import React from "react";
import { createPortal } from "react-dom";
import type { Editor } from "@tiptap/react";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowUpIcon, CheckIcon, PenLineIcon, RefreshCwIcon, SparklesIcon, Undo2Icon } from "lucide-react";
import toast from "react-hot-toast";
import useGenerateWrite from "@/lib/api/hooks/app/generation/useGenerateWrite";
import { usePermission } from "@/hooks/usePermission";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import formatUsage from "@/components/app/ai/usage";
import { useAnchoredFloating, caretReference } from "@/hooks/useAnchoredFloating";

const CONTEXT_WINDOW = 1500;

// Minimal plain-model-text → TipTap HTML (paragraphs on blank lines, hard breaks
// inside). Matches RichTextAIEdit's converter.
function plainToHTML(text: string): string {
    const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    return text
        .split(/\n{2,}/)
        .map((p) => `<p>${esc(p).replace(/\n/g, "<br>")}</p>`)
        .join("");
}

type Phase = "idle" | "busy" | "applied";

export default function RichTextAICaret({ editor }: { editor: Editor }) {
    const writeMut = useGenerateWrite();
    const canAI = usePermission("USE_AI");

    const [companion, setCompanion] = React.useState<{ top: number; left: number } | null>(null);
    const [open, setOpen] = React.useState(false);
    const [phase, setPhase] = React.useState<Phase>("idle");
    const [instruction, setInstruction] = React.useState("");
    const [usage, setUsage] = React.useState<{ charged: number; tokens: number } | null>(null);

    const frozenPos = React.useRef<number | null>(null);
    const lastRun = React.useRef<{ instruction: string; prevHTML: string } | null>(null);
    const inputRef = React.useRef<HTMLInputElement>(null);
    const localRef = React.useRef<HTMLDivElement | null>(null);
    const openRef = React.useRef(open);
    openRef.current = open;

    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 8,
    });

    // Anchor the open menu to the frozen caret via a floating-ui virtual element.
    React.useEffect(() => {
        if (!open) {
            setReference(null);
            return;
        }
        setReference(
            caretReference(() => {
                const pos = frozenPos.current ?? editor.state.selection.from;
                try {
                    const c = editor.view.coordsAtPos(pos);
                    return new DOMRect(c.left, c.top, 0, c.bottom - c.top);
                } catch {
                    return null;
                }
            }, editor.view.dom),
        );
    }, [open, editor, setReference]);

    // Track the caret-line companion position while the menu is closed.
    const measureCompanion = React.useCallback(() => {
        if (openRef.current) return;
        const { selection } = editor.state;
        if (!selection.empty || !editor.isFocused) {
            setCompanion(null);
            return;
        }
        try {
            const c = editor.view.coordsAtPos(selection.from);
            const box = editor.view.dom.getBoundingClientRect();
            setCompanion({ top: c.top, left: box.right - 28 });
        } catch {
            setCompanion(null);
        }
    }, [editor]);

    React.useEffect(() => {
        measureCompanion();
        editor.on("selectionUpdate", measureCompanion);
        editor.on("focus", measureCompanion);
        editor.on("blur", measureCompanion);
        editor.on("update", measureCompanion);
        const onScroll = () => measureCompanion();
        window.addEventListener("scroll", onScroll, true);
        window.addEventListener("resize", onScroll);
        return () => {
            editor.off("selectionUpdate", measureCompanion);
            editor.off("focus", measureCompanion);
            editor.off("blur", measureCompanion);
            editor.off("update", measureCompanion);
            window.removeEventListener("scroll", onScroll, true);
            window.removeEventListener("resize", onScroll);
        };
    }, [editor, measureCompanion]);

    // ⌘J at a collapsed caret opens the menu.
    React.useEffect(() => {
        const dom = editor.view.dom;
        const onKey = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "j") {
                if (!editor.state.selection.empty) return; // selection belongs to the edit pill
                e.preventDefault();
                frozenPos.current = editor.state.selection.from;
                lastRun.current = null;
                setPhase("idle");
                setInstruction("");
                setOpen(true);
            }
        };
        dom.addEventListener("keydown", onKey);
        return () => dom.removeEventListener("keydown", onKey);
    }, [editor]);

    // Click-away + Escape while open.
    React.useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent | TouchEvent) => {
            const t = e.target as Node | null;
            if (localRef.current?.contains(t)) return;
            if (editor.view.dom.contains(t as Node)) return;
            setOpen(false);
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.stopPropagation();
                setOpen(false);
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
    }, [open, editor]);

    React.useEffect(() => {
        if (open && phase === "idle") inputRef.current?.focus();
    }, [open, phase]);

    const run = React.useCallback(
        (raw: string) => {
            const pos = frozenPos.current;
            if (pos === null || writeMut.isPending) return;
            const prevHTML = editor.getHTML();
            const before = editor.state.doc.textBetween(Math.max(0, pos - CONTEXT_WINDOW), pos, "\n", " ");
            const prompt =
                raw +
                "\n\nYou are writing text to insert into an email draft at the cursor. Return ONLY the text to insert: no preamble, no subject line, no signature, no quotes around it. Match the draft's language and tone." +
                "\n\nThis is a campaign email template sent to many recipients. To personalize, use merge variables in Go-template form written EXACTLY, with the leading dot and double braces: {{.FirstName}}, {{.LastName}}, {{.Company}}, {{.Email}}, {{.Phone}}. Prefer a merge variable over a placeholder like [Name] or [Company]." +
                (before.trim() ? `\n\nDraft so far up to the cursor:\n${before}` : "");
            setPhase("busy");
            writeMut.mutate(
                { prompt },
                {
                    onSuccess: (res) => {
                        if (!openRef.current) return;
                        setUsage({ charged: res.credits_charged ?? 0, tokens: res.tokens_used ?? 0 });
                        editor.chain().focus().insertContentAt(pos, plainToHTML(res.text)).run();
                        lastRun.current = { instruction: raw, prevHTML };
                        setPhase("applied");
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
        [editor, writeMut],
    );

    const undo = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        editor.commands.setContent(last.prevHTML, { emitUpdate: true });
        lastRun.current = null;
        setPhase("idle");
    }, [editor]);

    const retry = React.useCallback(() => {
        const last = lastRun.current;
        if (!last) return;
        editor.commands.setContent(last.prevHTML, { emitUpdate: true });
        run(last.instruction);
    }, [editor, run]);

    const submit = () => {
        const text = instruction.trim();
        if (text) run(text);
    };

    if (typeof document === "undefined" || !canAI) return null;

    return createPortal(
        <>
            <AnimatePresence>
                {companion && !open && (
                    <motion.button
                        key="ai-caret-companion"
                        type="button"
                        initial={{ opacity: 0, scale: 0.8 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.9 }}
                        transition={{ duration: 0.12 }}
                        style={{ position: "fixed", top: companion.top - 1, left: companion.left, zIndex: 55 }}
                        title="Write with AI (⌘J)"
                        aria-label="Write with AI"
                        className="inline-flex size-[22px] items-center justify-center rounded-md text-slate-300 transition-colors hover:bg-sky-50 hover:text-sky-600"
                        onMouseDown={(e) => {
                            e.preventDefault();
                            frozenPos.current = editor.state.selection.from;
                            lastRun.current = null;
                            setPhase("idle");
                            setInstruction("");
                            setOpen(true);
                        }}
                    >
                        <SparklesIcon className="h-3.5 w-3.5" />
                    </motion.button>
                )}
                {open && (
                    <motion.div
                        key="ai-caret-menu"
                        ref={(el) => {
                            localRef.current = el;
                            setFloating(el);
                        }}
                        data-floating=""
                        style={floatingStyle}
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.12 }}
                        className="z-[60] w-80 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.22)]"
                    >
                        {phase === "busy" ? (
                            <div className="flex items-center gap-2 px-3 py-2.5">
                                <SparklesIcon className="h-3.5 w-3.5 shrink-0 animate-pulse text-sky-500" />
                                <span className="text-[12px] font-medium text-slate-600">Writing…</span>
                            </div>
                        ) : phase === "applied" ? (
                            <div className="flex items-center gap-1.5 px-2.5 py-2">
                                <span className="mr-auto inline-flex items-center gap-1.5 text-[12px] font-medium text-slate-900">
                                    <CheckIcon className="h-3.5 w-3.5 text-emerald-600" />
                                    Inserted
                                    {usage && formatUsage(usage.charged, usage.tokens) && (
                                        <span className="text-[10.5px] font-normal text-slate-400">
                                            · {formatUsage(usage.charged, usage.tokens)}
                                        </span>
                                    )}
                                </span>
                                <button
                                    type="button"
                                    onClick={undo}
                                    className="inline-flex h-6 items-center gap-1 rounded px-1.5 text-[11.5px] text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
                                >
                                    <Undo2Icon className="h-3 w-3" /> Undo
                                </button>
                                <button
                                    type="button"
                                    onClick={retry}
                                    className="inline-flex h-6 items-center gap-1 rounded px-1.5 text-[11.5px] text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900"
                                >
                                    <RefreshCwIcon className="h-3 w-3" /> Again
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setOpen(false)}
                                    className="h-6 rounded bg-slate-900 px-2 text-[11.5px] font-medium text-white transition-colors hover:bg-slate-800"
                                >
                                    Done
                                </button>
                            </div>
                        ) : (
                            <div>
                                <div className="flex items-center gap-1.5 px-2.5 pb-2 pt-2.5">
                                    <SparklesIcon className="h-3.5 w-3.5 shrink-0 text-sky-500" />
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
                                        className="h-7 min-w-0 flex-1 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                                    />
                                    <button
                                        type="button"
                                        onClick={submit}
                                        disabled={!instruction.trim()}
                                        aria-label="Write at the cursor"
                                        className="inline-flex size-6 items-center justify-center rounded-md bg-sky-600 text-white transition-colors hover:bg-sky-700 disabled:opacity-40"
                                    >
                                        <ArrowUpIcon className="h-3.5 w-3.5" />
                                    </button>
                                </div>
                                <div className="border-t border-slate-100 py-1">
                                    <button
                                        type="button"
                                        onClick={() =>
                                            run("Continue the draft naturally from the cursor, adding the next sentence or two.")
                                        }
                                        disabled={!editor.getText().trim()}
                                        className="flex h-8 w-full items-center gap-2 px-2.5 text-[12px] text-slate-700 transition-colors hover:bg-slate-50 disabled:opacity-40"
                                    >
                                        <PenLineIcon className="h-3.5 w-3.5 text-slate-400" />
                                        Continue writing
                                        <span className="ml-auto text-[10px] text-slate-300">from 1 credit</span>
                                    </button>
                                </div>
                            </div>
                        )}
                    </motion.div>
                )}
            </AnimatePresence>
        </>,
        document.body,
    );
}
