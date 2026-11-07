// useEditorAIRun streams AI text into a TipTap document: the words appear in
// place as the model writes them, tinted, with a caret at the end. When the
// model is done the passage waits for review: keep it, discard it (the
// document goes back to how it was before the first run), try the original
// ask again, or refine what is there with another instruction. A kept passage
// is one undo step.

import React from "react";
import type { Editor } from "@tiptap/react";
import type { Slice } from "@tiptap/pm/model";
import type { Transaction } from "@tiptap/pm/state";
import { reportAIError } from "./reportAIError";
import { markAIWritten } from "./aiWritten";
import { clampContext, passageAIConfigs, passageHTML, passageText, replacePassage, restoreEdges } from "./richTextPassage";
import useWritingStream, { isAbort, type WritingRequest } from "./useWritingStream";

export type EditorAIPhase = "idle" | "thinking" | "writing" | "checking" | "review";

export interface EditorAIRunSpec {
    from: number;
    to: number;
    request: WritingRequest;
    // What ends up selected: the caret after the text, or the passage itself.
    select: "end" | "range";
    // The text the request rewrites, so its edge whitespace survives.
    original?: string;
    aiConfigs?: Map<string, string>;
}

export interface EditorAIRun {
    phase: EditorAIPhase;
    usage: { chargedUsd: number; tokens: number } | null;
    // Words in the passage under review, and whether the last run changed it.
    words: number;
    changed: boolean;
    // Where the passage is now, for anchoring a card to its end.
    range: () => { from: number; to: number } | null;
    run: (spec: EditorAIRunSpec) => void;
    refine: (instruction: string) => void;
    retry: () => void;
    keep: () => void;
    discard: () => void;
    // Stops the model and keeps what it wrote so far, for review.
    stop: () => void;
}

const FENCES = /<<<(?:END_)?UNTRUSTED_CONTENT>>>/g;

const wordCount = (s: string) => (s.trim() ? s.trim().split(/\s+/).length : 0);

export default function useEditorAIRun(editor: Editor, onSettle?: () => void): EditorAIRun {
    const stream = useWritingStream();
    const [phase, setPhase] = React.useState<EditorAIPhase>("idle");
    const [usage, setUsage] = React.useState<EditorAIRun["usage"]>(null);
    const [words, setWords] = React.useState(0);
    const [changed, setChanged] = React.useState(true);

    // The document before the first run of this session, for Discard.
    const base = React.useRef<{ from: number; slice: Slice } | null>(null);
    const first = React.useRef<EditorAIRunSpec | null>(null);
    const range = React.useRef<{ from: number; to: number } | null>(null);
    const shown = React.useRef("");
    const runId = React.useRef(0);
    const phaseRef = React.useRef(phase);
    phaseRef.current = phase;
    const settle = React.useRef(onSettle);
    settle.current = onSettle;

    const shape = (spec: EditorAIRunSpec, text: string) => {
        const clean = text.replace(FENCES, "");
        return spec.original !== undefined ? restoreEdges(spec.original, clean.trim()) : clean;
    };

    const end = React.useCallback(() => {
        markAIWritten(editor, null);
        base.current = null;
        first.current = null;
        range.current = null;
        setPhase("idle");
        settle.current?.();
    }, [editor]);

    // Puts the passage back to before: this run's own text, or the session's.
    const restore = React.useCallback(
        (to: { from: number; slice: Slice }, history: boolean) => {
            const r = range.current;
            if (!r || editor.isDestroyed) return;
            const tr = editor.state.tr.replace(r.from, r.to, to.slice);
            if (!history) tr.setMeta("addToHistory", false);
            editor.view.dispatch(tr);
            range.current = { from: to.from, to: tr.mapping.map(r.to) };
        },
        [editor],
    );

    const run = React.useCallback(
        (spec: EditorAIRunSpec) => {
            const id = ++runId.current;
            const before = { from: spec.from, slice: editor.state.doc.slice(spec.from, spec.to) };
            base.current ??= before;
            first.current ??= spec;
            range.current = { from: spec.from, to: spec.to };
            shown.current = "";
            let wrote = false;
            setUsage(null);
            setPhase("thinking");
            editor.setEditable(false, false);

            // Lands text as the one undoable step of this run.
            const land = (text: string) => {
                restore(before, false);
                const html = passageHTML(text, spec.aiConfigs);
                const at = replacePassage(editor, spec.from, spec.to, html, { select: spec.select });
                range.current = { from: spec.from, to: at ?? spec.to };
                markAIWritten(editor, { ...range.current, writing: false });
                setWords(wordCount(text));
                setChanged(at !== null);
                setPhase("review");
            };

            stream
                .run(spec.request, {
                    onText: (partial) => {
                        if (runId.current !== id) return;
                        const r = range.current!;
                        shown.current = shape(spec, partial);
                        const at = replacePassage(editor, r.from, r.to, passageHTML(shown.current, spec.aiConfigs), {
                            history: false,
                            select: "end",
                        });
                        if (at !== null) range.current = { from: r.from, to: at };
                        markAIWritten(editor, { ...range.current!, writing: true });
                        if (!wrote) {
                            wrote = true;
                            setPhase("writing");
                        }
                    },
                    onChecking: () => runId.current === id && setPhase("checking"),
                })
                .then((res) => {
                    if (runId.current !== id) return;
                    editor.setEditable(true, false);
                    setUsage({ chargedUsd: res.charged_usd ?? 0, tokens: res.tokens_used ?? 0 });
                    land(shape(spec, res.text));
                })
                .catch((e) => {
                    if (runId.current !== id) return;
                    editor.setEditable(true, false);
                    if (isAbort(e) && shown.current.trim()) {
                        land(shown.current);
                        return;
                    }
                    if (!isAbort(e)) reportAIError(e);
                    restore(before, false);
                    // A refinement that failed leaves the earlier passage under review.
                    if (base.current !== before) {
                        markAIWritten(editor, { ...range.current!, writing: false });
                        setPhase("review");
                    } else end();
                });
        },
        [editor, end, restore, stream],
    );

    const discard = React.useCallback(() => {
        runId.current++;
        stream.stop();
        editor.setEditable(true, false);
        if (base.current) restore(base.current, true);
        end();
        editor.commands.focus();
    }, [editor, end, restore, stream]);

    const keep = React.useCallback(() => {
        const r = range.current;
        end();
        editor.commands.focus(r ? r.to : undefined);
    }, [editor, end]);

    const retry = React.useCallback(() => {
        const spec = first.current;
        const b = base.current;
        if (!spec || !b) return;
        restore(b, false);
        const r = range.current!;
        run({ ...spec, from: r.from, to: r.to });
    }, [restore, run]);

    const refine = React.useCallback(
        (instruction: string) => {
            const r = range.current;
            const spec = first.current;
            if (!r || !spec) return;
            const text = passageText(editor, r.from, r.to);
            run({
                from: r.from,
                to: r.to,
                select: spec.select,
                original: text,
                aiConfigs: passageAIConfigs(editor, r.from, r.to),
                request: {
                    path: "/generation/edit",
                    body: { text, instruction, context: clampContext(editor.getText()) },
                },
            });
        },
        [editor, run],
    );

    // Typing into the document while a passage waits for review keeps it.
    React.useEffect(() => {
        const onTr = ({ transaction }: { transaction: Transaction }) => {
            if (phaseRef.current === "review" && transaction.docChanged && transaction.getMeta("addToHistory") !== false) end();
        };
        editor.on("transaction", onTr);
        return () => {
            editor.off("transaction", onTr);
        };
    }, [editor, end]);

    React.useEffect(
        () => () => {
            runId.current++;
            if (!editor.isDestroyed) {
                editor.setEditable(true, false);
                markAIWritten(editor, null);
            }
        },
        [editor],
    );

    return {
        phase,
        usage,
        words,
        changed,
        range: () => range.current,
        run,
        refine,
        retry,
        keep,
        discard,
        stop: stream.stop,
    };
}
