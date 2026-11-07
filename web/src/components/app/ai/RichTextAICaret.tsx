// RichTextAICaret — the caret AI assistant for the campaign body (TipTap),
// mirroring the composer's TextareaAICaret so writing with AI feels identical
// everywhere. While the editor is focused with a collapsed caret, a faint sparkle
// sits in the right gutter aligned with the caret line; clicking it (or ⌘J) opens
// a small menu AT the caret to ask AI to write, or continue the draft. The text
// streams in at the caret as the model writes it, then waits for review in a
// menu by the end of it: keep, try again, adjust, or discard.
//
// Complements RichTextAIEdit (the non-collapsed-selection "Edit with AI" pill);
// the two are exclusive (this only renders for a collapsed caret).

import React from "react";
import { createPortal } from "react-dom";
import type { Editor } from "@tiptap/react";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowUpIcon, PenLineIcon, SparklesIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import useAiMetered from "@/hooks/useAiMetered";
import { useAnchoredFloating, caretReference } from "@/hooks/useAnchoredFloating";
import { passageText } from "./richTextPassage";
import { shortcutLabel } from "@/components/ui/shortcut-tooltip";
import useFocusOnOpen from "@/hooks/useFocusOnOpen";
import useClickOutside from "@/hooks/useClickOutside";
import useEditorSheen from "./useEditorSheen";
import useEditorAIRun from "./useEditorAIRun";
import AIRunCard from "./AIRunCard";
import { fmtUsdAbout } from "@/lib/money";
import { AI_ESTIMATE_USD } from "@/lib/aiEstimates";
import { AI_CARD_CLASS, WRITE_REFINEMENTS, aiRunCardClass } from "./aiCard";

const CONTEXT_WINDOW = 1500;

export default function RichTextAICaret({ editor }: { editor: Editor }) {
    const canAI = usePermission("USE_AI");
    const metered = useAiMetered();

    const [companion, setCompanion] = React.useState<{ top: number; left: number } | null>(null);
    const [open, setOpen] = React.useState(false);
    const [instruction, setInstruction] = React.useState("");

    const ai = useEditorAIRun(editor, () => setOpen(false));
    const busy = ai.phase === "thinking" || ai.phase === "writing" || ai.phase === "checking";

    const frozenPos = React.useRef<number | null>(null);
    const inputRef = React.useRef<HTMLInputElement>(null);
    const localRef = React.useRef<HTMLDivElement | null>(null);
    const openRef = React.useRef(open);
    openRef.current = open;
    const aiRef = React.useRef(ai);
    aiRef.current = ai;

    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 8,
        follow: busy,
    });

    // Anchor to the end of what AI is writing, or to the frozen caret before it starts.
    React.useEffect(() => {
        if (!open) {
            setReference(null);
            return;
        }
        setReference(
            caretReference(() => {
                const pos = aiRef.current.range()?.to ?? frozenPos.current ?? editor.state.selection.from;
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

    const openMenu = React.useCallback(() => {
        if (aiRef.current.phase !== "idle") return;
        frozenPos.current = editor.state.selection.from;
        setInstruction("");
        setOpen(true);
    }, [editor]);

    // ⌘J at a collapsed caret opens the menu.
    React.useEffect(() => {
        const dom = editor.view.dom;
        const onKey = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key?.toLowerCase() === "j") {
                if (!editor.state.selection.empty) return; // selection belongs to the edit pill
                e.preventDefault();
                openMenu();
            }
        };
        dom.addEventListener("keydown", onKey);
        return () => dom.removeEventListener("keydown", onKey);
    }, [editor, openMenu]);

    // Escape steps back: stop the writing, then discard it, then close.
    // Clicking away keeps a finished passage and leaves a running one running.
    const editorDom = React.useMemo(() => ({ get current() { return editor.view.dom; } }), [editor]);
    useClickOutside(
        open,
        (cause) => {
            const run = aiRef.current;
            if (cause === "escape") {
                if (run.phase === "writing") run.stop();
                else if (run.phase !== "idle") run.discard();
                else {
                    setOpen(false);
                    editor.commands.focus();
                }
            } else if (run.phase === "review") run.keep();
            else if (run.phase === "idle") setOpen(false);
            // Each path above leaves focus where it belongs.
            return false;
        },
        [editorDom, localRef],
    );

    useFocusOnOpen(open && ai.phase === "idle", inputRef);
    useEditorSheen(editor.view.dom, ai.phase === "thinking");

    const run = (raw: string) => {
        const pos = frozenPos.current;
        if (pos === null || ai.phase !== "idle") return;
        const before = passageText(editor, Math.max(0, pos - CONTEXT_WINDOW), pos);
        const prompt =
            raw +
            "\n\nYou are writing text to insert into an email draft at the cursor. Return ONLY the text to insert: no preamble, no subject line, no signature, no quotes around it. Match the draft's language and tone." +
            "\n\nThis is a campaign email template sent to many recipients. To personalize, use merge variables in Go-template form written EXACTLY, with the leading dot and double braces: {{.FirstName}}, {{.LastName}}, {{.Company}}, {{.Email}}, {{.Phone}}. Prefer a merge variable over a placeholder like [Name] or [Company]." +
            (before.trim() ? `\n\nDraft so far up to the cursor:\n${before}` : "");
        ai.run({ from: pos, to: pos, select: "end", request: { path: "/generation/write", body: { prompt } } });
    };

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
                        style={{ position: "fixed", top: companion.top - 1, left: companion.left, zIndex: 145 }}
                        title={`Write with AI (${shortcutLabel("mod+J")})`}
                        aria-label="Write with AI"
                        className="inline-flex size-[22px] items-center justify-center rounded-md text-slate-300 transition-colors hover:bg-sky-50 hover:text-sky-600"
                        onMouseDown={(e) => {
                            e.preventDefault();
                            openMenu();
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
                        className={`z-[150] ${ai.phase === "idle" ? `w-80 ${AI_CARD_CLASS}` : aiRunCardClass(ai.phase)}`}
                    >
                        {ai.phase !== "idle" ? (
                            <AIRunCard run={ai} thinking="Reading your draft…" writing="Writing…" refinements={WRITE_REFINEMENTS} />
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
                                        {metered && <span className="ml-auto text-[10px] text-slate-300">{fmtUsdAbout(AI_ESTIMATE_USD.writingAssistant)}</span>}
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
