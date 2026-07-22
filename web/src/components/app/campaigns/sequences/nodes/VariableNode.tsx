// VariableNode — an atomic inline chip for a merge tag like {{.Company}}. The
// node keeps the FULL literal token as the span's text content, so:
//   - editor.getHTML() -> `<span data-var>{{.Company}}</span>`, which the Go
//     send-time renderer (internal/tasks/template.go) resolves unchanged, and
//   - htmlToPlain()'s naive tag-strip still yields the literal token for
//     body_plain.
// parseHTML matches span[data-var]; upgradeVariableTokens (@/lib/templateVars)
// turns previously-saved plain {{.X}} text into chips on load. Click-to-edit is a
// floating-ui-anchored popover portaled to <body> (the node view lives inside the
// ProseMirror editable, where an inline <input> fights contentEditable).

import React from "react";
import { createPortal } from "react-dom";
import { Node as TiptapNode, mergeAttributes, nodeInputRule } from "@tiptap/core";
import { ReactNodeViewRenderer, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { AnimatePresence, motion } from "framer-motion";
import { BracesIcon, XIcon, CheckIcon } from "lucide-react";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import { useAnchoredFloating } from "@/hooks/useAnchoredFloating";
import {
    STANDARD_VARS,
    buildToken,
    parseToken,
    tokenLabel,
    isStandardKey,
    cleanFieldName,
} from "@/lib/templateVars";

declare module "@tiptap/core" {
    interface Commands<ReturnType> {
        variable: {
            // Insert a merge-tag chip carrying the given literal token.
            insertVariable: (token: string) => ReturnType;
        };
    }
}

export const VariableNode = TiptapNode.create({
    name: "variable",
    inline: true,
    group: "inline",
    atom: true,
    selectable: true,

    addAttributes() {
        return {
            token: {
                default: "",
                parseHTML: (el) => (el as HTMLElement).textContent || "",
                renderHTML: () => ({}),
            },
        };
    },

    parseHTML() {
        return [
            {
                tag: "span[data-var]",
                getAttrs: (el) => {
                    const token = ((el as HTMLElement).textContent || "").trim();
                    return token ? { token } : false;
                },
            },
        ];
    },

    renderHTML({ node, HTMLAttributes }) {
        return ["span", mergeAttributes(HTMLAttributes, { "data-var": "" }), node.attrs.token];
    },

    renderText({ node }) {
        return node.attrs.token;
    },

    addCommands() {
        return {
            insertVariable:
                (token: string) =>
                ({ chain }) =>
                    chain().insertContent({ type: this.name, attrs: { token } }).run(),
        };
    },

    addInputRules() {
        return [
            nodeInputRule({
                find: /\{\{\s*\.([A-Za-z0-9_ -]+?)\s*\}\}$/,
                type: this.type,
                getAttributes: (match) => ({ token: buildToken(match[1]) }),
            }),
        ];
    },

    addNodeView() {
        return ReactNodeViewRenderer(VariableChip);
    },
});

// Compact chip with a floating-ui-anchored click-to-edit popover (swap field, set
// a fallback, remove). floating-ui keeps it glued to the chip through scroll.
function VariableChip({ node, updateAttributes, deleteNode, selected, editor, getPos }: NodeViewProps) {
    const token: string = node.attrs.token || "";
    const parsed = parseToken(token);
    const [open, setOpen] = React.useState(false);
    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 6,
        maxHeight: true,
    });

    // Free raw edit: if the raw text is a plain field token, keep it a chip;
    // otherwise replace the atom node with the raw literal so any expression is
    // possible ("edit what is inside the {{}} freely").
    const onRaw = React.useCallback(
        (raw: string) => {
            const t = raw.trim();
            setOpen(false);
            if (!t) return;
            if (parseToken(t)) {
                updateAttributes({ token: t });
                return;
            }
            const pos = typeof getPos === "function" ? getPos() : null;
            if (typeof pos === "number") {
                editor.chain().focus().insertContentAt({ from: pos, to: pos + node.nodeSize }, t).run();
            }
        },
        [editor, getPos, node, updateAttributes],
    );

    return (
        <NodeViewWrapper as="span" className="tpl-var-wrap">
            <motion.button
                ref={(el) => setReference(el)}
                type="button"
                initial={{ scale: 0.92, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                whileTap={{ scale: 0.96 }}
                transition={{ type: "spring", stiffness: 640, damping: 30 }}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => setOpen((o) => !o)}
                title={token}
                className={`tpl-var ${selected || open ? "tpl-var-active" : ""}`}
            >
                <BracesIcon className="h-2.5 w-2.5 shrink-0 opacity-70" />
                {tokenLabel(token)}
            </motion.button>
            {typeof document !== "undefined" &&
                createPortal(
                    <AnimatePresence>
                        {open && (
                            <VariableChipEditor
                                setFloating={setFloating}
                                floatingStyle={floatingStyle}
                                token={token}
                                currentKey={parsed?.key ?? cleanFieldName(token)}
                                fallback={parsed?.fallback ?? ""}
                                onChange={(next) => {
                                    updateAttributes({ token: next });
                                    setOpen(false);
                                }}
                                onRaw={onRaw}
                                onRemove={() => {
                                    deleteNode();
                                    setOpen(false);
                                }}
                                onClose={() => setOpen(false)}
                            />
                        )}
                    </AnimatePresence>,
                    document.body,
                )}
        </NodeViewWrapper>
    );
}

function VariableChipEditor({
    setFloating,
    floatingStyle,
    token,
    currentKey,
    fallback,
    onChange,
    onRaw,
    onRemove,
    onClose,
}: {
    setFloating: (el: HTMLElement | null) => void;
    floatingStyle: React.CSSProperties;
    token: string;
    currentKey: string;
    fallback: string;
    onChange: (next: string) => void;
    onRaw: (raw: string) => void;
    onRemove: () => void;
    onClose: () => void;
}) {
    const { data: customKeys = [] } = useCustomFieldKeys();
    const [fb, setFb] = React.useState(fallback);
    const localRef = React.useRef<HTMLDivElement | null>(null);
    const parseable = parseToken(token) !== null;
    // Raw mode exposes the literal {{...}} for free editing; auto-on for tokens
    // the structured UI can't model (helpers, complex expressions).
    const [showRaw, setShowRaw] = React.useState(!parseable);
    const [raw, setRaw] = React.useState(token);

    const setRefs = React.useCallback(
        (el: HTMLDivElement | null) => {
            localRef.current = el;
            setFloating(el);
        },
        [setFloating],
    );

    React.useEffect(() => {
        const onDown = (e: MouseEvent | TouchEvent) => {
            if (!localRef.current?.contains(e.target as Node)) onClose();
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.stopPropagation();
                onClose();
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
    }, [onClose]);

    const options = [
        ...STANDARD_VARS.map((v) => ({ key: v.key, label: v.label })),
        ...customKeys.filter((k) => !isStandardKey(k)).map((k) => ({ key: k, label: k })),
    ];

    return (
        <motion.div
            ref={setRefs}
            data-floating=""
            style={floatingStyle}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.1 }}
            className="z-[60] w-64 overflow-hidden rounded-lg border border-slate-200 bg-white p-2 text-left shadow-[0_10px_30px_-10px_rgba(15,23,42,0.25)]"
        >
            <div className="flex items-center justify-between px-0.5 pb-1">
                <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                    {showRaw ? "Raw token" : "Field"}
                </span>
                <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => setShowRaw((v) => !v)}
                    className="rounded px-1 font-mono text-[10px] text-slate-400 transition-colors hover:text-sky-600"
                    title={showRaw ? "Use the field picker" : "Edit the raw token freely"}
                >
                    {showRaw ? "field picker" : "{ } edit raw"}
                </button>
            </div>
            {showRaw ? (
                <div>
                    <textarea
                        value={raw}
                        onChange={(e) => setRaw(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                                e.preventDefault();
                                onRaw(raw);
                            }
                        }}
                        rows={2}
                        spellCheck={false}
                        className="w-full resize-y rounded-md border border-slate-200 px-2 py-1.5 font-mono text-[11.5px] text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                    <div className="mt-1 flex items-center justify-between">
                        <span className="text-[10px] text-slate-400">Any {"{{…}}"} expression</span>
                        <button
                            type="button"
                            onMouseDown={(e) => e.preventDefault()}
                            onClick={() => onRaw(raw)}
                            className="h-7 rounded-md bg-slate-900 px-2.5 text-[11.5px] font-medium text-white transition-colors hover:bg-slate-800"
                        >
                            Apply
                        </button>
                    </div>
                </div>
            ) : (
                <>
                    <div className="max-h-44 space-y-0.5 overflow-y-auto">
                        {options.map((o) => {
                            const active =
                                cleanFieldName(o.key).toLowerCase() === cleanFieldName(currentKey).toLowerCase();
                            return (
                                <button
                                    key={o.key}
                                    type="button"
                                    onMouseDown={(e) => e.preventDefault()}
                                    onClick={() => onChange(buildToken(o.key, fb))}
                                    className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1 text-left text-[12px] transition-colors ${
                                        active ? "bg-sky-50 text-sky-700" : "text-slate-700 hover:bg-slate-100"
                                    }`}
                                >
                                    <span className="truncate">{o.label}</span>
                                    {active && <CheckIcon className="h-3 w-3 shrink-0" />}
                                </button>
                            );
                        })}
                    </div>
                    <div className="mt-1.5 border-t border-slate-100 pt-1.5">
                        <div className="px-0.5 pb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                            Fallback if blank
                        </div>
                        <div className="flex items-center gap-1.5">
                            <input
                                value={fb}
                                onChange={(e) => setFb(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        onChange(buildToken(currentKey, fb));
                                    }
                                }}
                                placeholder='e.g. "there"'
                                className="h-7 min-w-0 flex-1 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                            />
                            <button
                                type="button"
                                onMouseDown={(e) => e.preventDefault()}
                                onClick={() => onChange(buildToken(currentKey, fb))}
                                className="h-7 shrink-0 rounded-md bg-slate-900 px-2 text-[11.5px] font-medium text-white transition-colors hover:bg-slate-800"
                            >
                                Apply
                            </button>
                        </div>
                    </div>
                </>
            )}
            <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={onRemove}
                className="mt-1.5 flex w-full items-center gap-1.5 rounded px-2 py-1 text-[12px] text-slate-500 transition-colors hover:bg-rose-50 hover:text-rose-600"
            >
                <XIcon className="h-3 w-3" /> Remove
            </button>
        </motion.div>
    );
}
