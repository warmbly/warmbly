// AIVariableNode — an atomic inline "AI block" that generates unique copy for
// each recipient at send time. The full config rides inline in the editor HTML
// (base64 in a data attribute); the control-plane resolver reads it, renders the
// prompt per contact through the platform humanizer, and swaps the whole span for
// the result (see @/lib/aiVariables). The chip shows the prompt; clicking it opens
// a centered config modal (the app Dialog). The modal auto-opens ONCE right after
// insertion (via the justInserted registry), never again on Edit/Preview remounts.
//
// The instruction is the SAME rich editor used everywhere else, in `minimal` mode:
// {{.Field}} merge tokens render as chips, the {{ type-ahead works, and if/else
// conditionals are available — just small. The dialog runs non-modal so the
// editor's own body-portaled popovers (variable menu, type-ahead, condition
// builder) stay interactive inside it.

import React from "react";
import { createPortal } from "react-dom";
import { Node as TiptapNode, mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { AnimatePresence, motion } from "framer-motion";
import { SparklesIcon, GlobeIcon, TrashIcon } from "lucide-react";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useGenerateAIVariable from "@/lib/api/hooks/app/generation/useGenerateAIVariable";
import useTypewriter from "@/components/app/ai/useTypewriter";
import formatUsage from "@/components/app/ai/usage";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { WRITE_TONES } from "@/lib/api/models/app/generation/Write";
import { VARIABLES } from "@/lib/templateVars";
import RichTextEditor from "@/components/app/campaigns/sequences/RichTextEditor";
import { htmlToPlain, promptToHtml, renderPreview, SAMPLE } from "@/components/app/campaigns/sequences/emailPreview";
import { markJustInserted, consumeJustInserted } from "./justInserted";
import {
    type AIVariableConfig,
    DEFAULT_AI_CONFIG,
    aiToken,
    newAIVariableId,
    encodeConfig,
    decodeConfig,
} from "@/lib/aiVariables";

declare module "@tiptap/core" {
    interface Commands<ReturnType> {
        aiVariable: {
            // Insert a fresh AI block; opens focused for immediate configuration.
            insertAIVariable: (config?: Partial<AIVariableConfig>) => ReturnType;
        };
    }
}

export const AIVariableNode = TiptapNode.create({
    name: "aiVariable",
    inline: true,
    group: "inline",
    atom: true,
    selectable: true,
    draggable: false,

    addAttributes() {
        return {
            id: {
                default: "",
                parseHTML: (el) => (el as HTMLElement).getAttribute("data-ai-var") || "",
                renderHTML: (attrs) => ({ "data-ai-var": attrs.id }),
            },
            config: {
                default: DEFAULT_AI_CONFIG,
                parseHTML: (el) => decodeConfig((el as HTMLElement).getAttribute("data-ai-config") || ""),
                renderHTML: (attrs) => ({ "data-ai-config": encodeConfig(attrs.config as AIVariableConfig) }),
            },
        };
    },

    parseHTML() {
        return [{ tag: "span[data-ai-var]" }];
    },

    renderHTML({ node, HTMLAttributes }) {
        return ["span", mergeAttributes(HTMLAttributes), aiToken(node.attrs.id)];
    },

    renderText({ node }) {
        return aiToken(node.attrs.id);
    },

    addCommands() {
        return {
            insertAIVariable:
                (config?: Partial<AIVariableConfig>) =>
                ({ chain }) => {
                    // Mint the id here so it can be flagged as freshly inserted; the
                    // node view opens its modal once for this id and never again.
                    const id = newAIVariableId();
                    markJustInserted(id);
                    return chain()
                        .insertContent({
                            type: this.name,
                            attrs: { id, config: { ...DEFAULT_AI_CONFIG, ...config } },
                        })
                        .run();
                },
        };
    },

    addNodeView() {
        return ReactNodeViewRenderer(AIVariableChip);
    },
});

function truncate(s: string, n: number): string {
    const t = s.trim().replace(/\s+/g, " ");
    return t.length > n ? t.slice(0, n - 1) + "…" : t;
}

// Sky-tinted chip that shows the prompt; opens the config modal on click. It
// auto-opens only for a just-inserted block (consumeJustInserted), so toggling
// the Edit/Preview tabs — which remounts every node view — never reopens it.
function AIVariableChip({ node, updateAttributes, deleteNode, selected, editor }: NodeViewProps) {
    const config: AIVariableConfig = node.attrs.config || DEFAULT_AI_CONFIG;
    const [open, setOpen] = React.useState(() => consumeJustInserted(node.attrs.id));

    // The email text on both sides of this block, so generation writes a fragment
    // that flows with the sentence it lands in. Other AI blocks blank to a neutral
    // placeholder; captured lazily (read when the config modal opens).
    const getContext = React.useCallback(() => {
        try {
            const plain = htmlToPlain(editor.getHTML());
            const idx = plain.indexOf(aiToken(node.attrs.id));
            if (idx < 0) return { before: "", after: "" };
            const clean = (s: string) => s.replace(/\[\[ai:[^\]]*\]\]/g, "…").trim();
            return { before: clean(plain.slice(0, idx)), after: clean(plain.slice(idx + aiToken(node.attrs.id).length)) };
        } catch {
            return { before: "", after: "" };
        }
    }, [editor, node.attrs.id]);

    const label = config.name?.trim() || (config.prompt ? truncate(config.prompt, 44) : "") || "Set up AI block";

    return (
        <NodeViewWrapper as="span" className="tpl-ai-wrap">
            <motion.button
                type="button"
                initial={{ scale: 0.92, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                whileTap={{ scale: 0.96 }}
                transition={{ type: "spring", stiffness: 640, damping: 30 }}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => setOpen(true)}
                title={config.prompt ? `AI: ${config.prompt}` : "Configure this AI block"}
                className={`tpl-ai ${selected || open ? "tpl-ai-active" : ""} ${config.prompt ? "" : "tpl-ai-empty"}`}
            >
                <SparklesIcon className="h-2.5 w-2.5 shrink-0" />
                <span className="max-w-[16rem] truncate">{label}</span>
            </motion.button>
            {/* Non-modal so the embedded editor's body-portaled popovers (variable menu,
                {{ type-ahead, condition builder) stay clickable and focusable; the
                onInteractOutside guard keeps a click on one of those from closing it. */}
            <Dialog open={open} onOpenChange={setOpen} modal={false}>
                {/* Non-modal Radix renders no overlay, so paint our own dimming backdrop.
                    Clicking it lands outside the content and dismisses via Radix. */}
                {open &&
                    createPortal(
                        <div className="fixed inset-0 z-40 bg-black/50 duration-200 animate-in fade-in-0" aria-hidden />,
                        document.body,
                    )}
                <DialogContent
                    showCloseButton={false}
                    className="gap-0 overflow-hidden p-0 sm:max-w-[640px]"
                    onOpenAutoFocus={(e) => e.preventDefault()}
                    onInteractOutside={(e) => {
                        const target = e.detail.originalEvent.target as HTMLElement | null;
                        if (target?.closest("[data-floating]")) e.preventDefault();
                    }}
                >
                    <DialogTitle className="sr-only">Configure AI block</DialogTitle>
                    {open && (
                        <AIVariableConfigBody
                            config={config}
                            getContext={getContext}
                            onChange={(next) => updateAttributes({ config: next })}
                            onRemove={() => {
                                deleteNode();
                                setOpen(false);
                            }}
                            onClose={() => setOpen(false)}
                        />
                    )}
                </DialogContent>
            </Dialog>
        </NodeViewWrapper>
    );
}

function AIVariableConfigBody({
    config,
    getContext,
    onChange,
    onRemove,
    onClose,
}: {
    config: AIVariableConfig;
    getContext: () => { before: string; after: string };
    onChange: (next: AIVariableConfig) => void;
    onRemove: () => void;
    onClose: () => void;
}) {
    const gen = useGenerateAIVariable();
    const typewriter = useTypewriter();

    const [draft, setDraft] = React.useState<AIVariableConfig>(config);
    const [preview, setPreview] = React.useState<string>("");
    const [usage, setUsage] = React.useState<{ charged: number; tokens: number } | null>(null);
    // Surrounding email, captured when the modal opens (the body isn't edited while open).
    const [ctx] = React.useState(getContext);

    // Compute the editor's starting HTML once (the body remounts each time the
    // dialog opens), so the editor is effectively uncontrolled after mount and we
    // only read plain text back out; feeding derived HTML back would reset the
    // caret on every keystroke.
    const [initialHtml] = React.useState(() => promptToHtml(config.prompt));

    const patch = React.useCallback(
        (p: Partial<AIVariableConfig>) => {
            setDraft((d) => {
                const next = { ...d, ...p };
                onChange(next);
                return next;
            });
        },
        [onChange],
    );

    const runPreview = () => {
        if (!draft.prompt.trim() || gen.isPending) return;
        setPreview("");
        // mode is pinned "instant"; the removed research mode no longer exists.
        gen.mutate(
            {
                mode: "instant",
                prompt: draft.prompt,
                tone: draft.tone || undefined,
                web_search: draft.web_search,
                context_before: ctx.before,
                context_after: ctx.after,
            },
            {
                onSuccess: (res) => {
                    setUsage({ charged: res.credits_charged ?? 0, tokens: res.tokens_used ?? 0 });
                    typewriter.run(res.text, (partial) => setPreview(partial));
                },
                onError: (e) => {
                    const err = e as unknown as AppError;
                    if (err?.status === 402) {
                        toast.error("You're out of AI credits. Upgrade or purchase more to preview AI blocks.");
                    } else {
                        toast.error(buildError(err));
                    }
                },
            },
        );
    };

    return (
        <div className="flex max-h-[calc(100dvh-4rem)] min-h-[340px]">
            {/* LEFT — the instruction, in the same editor used everywhere else */}
            <div className="flex min-w-0 flex-1 flex-col">
                <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 pb-4 pt-5">
                    <div>
                        {/* opening clause — bare sparkle, no badge */}
                        <div className="mb-2 flex items-center gap-1.5 leading-none">
                            <SparklesIcon className="h-3.5 w-3.5 shrink-0 text-sky-500" />
                            <span className="text-[13px] text-slate-500">For each recipient, write…</span>
                        </div>
                        <RichTextEditor
                            minimal
                            html={initialHtml}
                            onChange={(html) => patch({ prompt: htmlToPlain(html) })}
                            variables={VARIABLES}
                            placeholder="a warm one-line opener for {{.FirstName}} at {{.Company}}"
                        />
                    </div>

                    {/* tone — quiet chips, all visible */}
                    <div className="flex flex-wrap items-center gap-1.5">
                        <span className="mr-1 text-[11px] text-slate-500">Tone</span>
                        {WRITE_TONES.map((t) => {
                            const active = (draft.tone || "") === t.value;
                            return (
                                <button
                                    key={t.value || "default"}
                                    type="button"
                                    onClick={() => patch({ tone: t.value })}
                                    className={`inline-flex h-6 items-center rounded-full border px-2.5 text-[11.5px] transition-colors ${
                                        active
                                            ? "border-sky-300 bg-sky-50 text-sky-700"
                                            : "border-slate-200 text-slate-500 hover:bg-slate-50"
                                    }`}
                                >
                                    {t.label}
                                </button>
                            );
                        })}
                    </div>

                    {/* the ONE add-on — a switch (globe left, switch on the right) */}
                    <button
                        type="button"
                        role="switch"
                        aria-checked={draft.web_search}
                        onClick={() => patch({ web_search: !draft.web_search })}
                        className="flex w-full items-center gap-2.5 text-left"
                    >
                        <GlobeIcon className="h-4 w-4 shrink-0 text-slate-400" />
                        <span className="min-w-0 flex-1">
                            <span className="block text-[12.5px] font-medium text-slate-700">Web search</span>
                            <span className="block text-[11px] leading-snug text-slate-400">
                                Look the contact up on the web before writing.
                            </span>
                        </span>
                        <span
                            className={`relative h-[18px] w-8 shrink-0 rounded-full transition-colors ${
                                draft.web_search ? "bg-sky-500" : "bg-slate-200"
                            }`}
                        >
                            <motion.span
                                layout
                                transition={{ type: "spring", stiffness: 560, damping: 34 }}
                                className={`absolute top-0.5 size-3.5 rounded-full bg-white shadow ${
                                    draft.web_search ? "right-0.5" : "left-0.5"
                                }`}
                            />
                        </span>
                    </button>

                    {/* cost — honest: it's metered by usage, not a flat number */}
                    <p className="text-[11px] leading-snug text-slate-400">
                        Billed by usage — the tokens each snippet uses{draft.web_search ? ", plus the web search" : ""}.
                        Preview to see a real example.
                    </p>
                </div>

                {/* actions — scoped to the config pane */}
                <div className="flex items-center justify-between border-t border-slate-200 px-5 py-3">
                    <button
                        type="button"
                        onClick={onRemove}
                        className="inline-flex items-center gap-1.5 text-[12px] text-slate-500 transition-colors hover:text-rose-600"
                    >
                        <TrashIcon className="h-3.5 w-3.5" /> Remove
                    </button>
                    <button
                        type="button"
                        onClick={onClose}
                        className="h-7 rounded-md bg-slate-900 px-4 text-[12.5px] font-medium text-white transition-colors hover:bg-slate-700"
                    >
                        Done
                    </button>
                </div>
            </div>

            {/* RIGHT — the live sample */}
            <div className="flex w-[300px] shrink-0 flex-col border-l border-slate-200 bg-slate-50/60 p-4">
                <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                    Sample for Alex Rivera at Acme
                </span>

                <div className="mt-3 min-h-0 flex-1 overflow-y-auto">
                    <AnimatePresence mode="wait">
                        {gen.isPending && !preview ? (
                            <motion.div
                                key="busy"
                                initial={{ opacity: 0 }}
                                animate={{ opacity: 1 }}
                                exit={{ opacity: 0 }}
                                className="flex items-center gap-2"
                            >
                                <SparklesIcon className="h-3.5 w-3.5 shrink-0 animate-pulse text-sky-500" />
                                <span className="ai-shimmer-text text-[12px] font-medium">Writing a sample…</span>
                            </motion.div>
                        ) : preview ? (
                            // Show the WHOLE message with the generated fragment in place, so
                            // you can see it actually fits. Surrounding text renders with the
                            // sample contact's values; the AI part is highlighted.
                            <motion.p
                                key="text"
                                initial={{ opacity: 0 }}
                                animate={{ opacity: 1 }}
                                className="whitespace-pre-wrap text-[13px] leading-relaxed text-slate-700"
                            >
                                {ctx.before || ctx.after ? (
                                    <>
                                        {renderPreview(ctx.before, SAMPLE)}
                                        {ctx.before ? " " : ""}
                                        <mark className="rounded bg-sky-100 px-0.5 text-slate-900">{preview}</mark>
                                        {ctx.after ? " " : ""}
                                        {renderPreview(ctx.after, SAMPLE)}
                                    </>
                                ) : (
                                    preview
                                )}
                            </motion.p>
                        ) : (
                            <p className="text-[12.5px] leading-relaxed text-slate-400">Your snippet appears here.</p>
                        )}
                    </AnimatePresence>
                    {usage && preview && formatUsage(usage.charged, usage.tokens) && (
                        <p className="mt-2 text-[10px] text-slate-400">{formatUsage(usage.charged, usage.tokens)}</p>
                    )}
                </div>

                <button
                    type="button"
                    onClick={runPreview}
                    disabled={!draft.prompt.trim() || gen.isPending}
                    className="mt-3 inline-flex h-7 items-center gap-1.5 self-start rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 disabled:opacity-50"
                >
                    <SparklesIcon className="h-3 w-3" />
                    {gen.isPending ? "Writing…" : preview ? "Try again" : "Preview"}
                </button>
            </div>
        </div>
    );
}
