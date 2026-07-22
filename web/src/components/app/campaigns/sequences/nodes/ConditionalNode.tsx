// ConditionalNode — an atomic inline chip for a Go-template conditional
// {{if EXPR}}THEN{{else}}ELSE{{end}}. The whole construct (condition + bodies)
// is edited in a builder popover, so the editor never carries fragile, orphanable
// half-tokens. It serializes as the literal template text (the span's content),
// so the send-time Go renderer resolves it unchanged and htmlToPlain keeps it.
// A raw-expression field is the escape hatch, so any condition is possible.

import React from "react";
import { createPortal } from "react-dom";
import { Node as TiptapNode, mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { AnimatePresence, motion } from "framer-motion";
import { GitBranchIcon, XIcon, ChevronDownIcon, PlusIcon } from "lucide-react";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import { useAnchoredFloating } from "@/hooks/useAnchoredFloating";
import { STANDARD_VARS, buildToken, cleanFieldName, isStandardKey } from "@/lib/templateVars";
import { markJustInserted, consumeJustInserted, freshId } from "./justInserted";

interface ConditionalAttrs {
    expr: string; // the condition after `{{if `, e.g. `.Company` or `eq .Industry "SaaS"`
    thenText: string;
    elseText: string;
    uid: string; // transient, not serialized — flags a just-inserted chip to open once
}

const DEFAULT_ATTRS: ConditionalAttrs = { expr: ".Company", thenText: "", elseText: "", uid: "" };

declare module "@tiptap/core" {
    interface Commands<ReturnType> {
        conditional: {
            insertConditional: (attrs?: Partial<ConditionalAttrs>) => ReturnType;
        };
    }
}

// buildConditionalToken assembles the literal template text from the attrs.
function buildConditionalToken(a: ConditionalAttrs): string {
    const expr = a.expr.trim() || ".Company";
    const then = a.thenText ?? "";
    if (a.elseText && a.elseText.trim()) {
        return `{{if ${expr}}}${then}{{else}}${a.elseText}{{end}}`;
    }
    return `{{if ${expr}}}${then}{{end}}`;
}

// parseConditionalToken reverses buildConditionalToken so a saved/pasted
// conditional round-trips back into the builder.
function parseConditionalToken(token: string): ConditionalAttrs | null {
    const m = token.match(/^\{\{\s*if\s+([\s\S]*?)\s*\}\}([\s\S]*?)(?:\{\{\s*else\s*\}\}([\s\S]*?))?\{\{\s*end\s*\}\}$/);
    if (!m) return null;
    return { expr: m[1].trim(), thenText: m[2] ?? "", elseText: m[3] ?? "" };
}

export const ConditionalNode = TiptapNode.create({
    name: "conditional",
    inline: true,
    group: "inline",
    atom: true,
    selectable: true,

    addAttributes() {
        return {
            expr: {
                default: DEFAULT_ATTRS.expr,
                parseHTML: (el) => parseConditionalToken((el as HTMLElement).textContent || "")?.expr ?? DEFAULT_ATTRS.expr,
                renderHTML: () => ({}),
            },
            thenText: {
                default: "",
                parseHTML: (el) => parseConditionalToken((el as HTMLElement).textContent || "")?.thenText ?? "",
                renderHTML: () => ({}),
            },
            elseText: {
                default: "",
                parseHTML: (el) => parseConditionalToken((el as HTMLElement).textContent || "")?.elseText ?? "",
                renderHTML: () => ({}),
            },
            // Transient: never serialized (regenerates as "" on load), so only a
            // fresh toolbar insert carries a uid the chip opens its builder for.
            uid: {
                default: "",
                parseHTML: () => "",
                renderHTML: () => ({}),
            },
        };
    },

    parseHTML() {
        return [
            {
                tag: "span[data-if]",
                getAttrs: (el) => {
                    const parsed = parseConditionalToken(((el as HTMLElement).textContent || "").trim());
                    return parsed ? parsed : false;
                },
            },
        ];
    },

    renderHTML({ node, HTMLAttributes }) {
        return ["span", mergeAttributes(HTMLAttributes, { "data-if": "" }), buildConditionalToken(node.attrs as ConditionalAttrs)];
    },

    renderText({ node }) {
        return buildConditionalToken(node.attrs as ConditionalAttrs);
    },

    addCommands() {
        return {
            insertConditional:
                (attrs?: Partial<ConditionalAttrs>) =>
                ({ chain }) => {
                    // Flag this insertion so the chip opens its builder exactly once.
                    const uid = freshId();
                    markJustInserted(uid);
                    return chain().insertContent({ type: this.name, attrs: { ...DEFAULT_ATTRS, ...attrs, uid } }).run();
                },
        };
    },

    addNodeView() {
        return ReactNodeViewRenderer(ConditionalChip);
    },
});

function clip(s: string, n: number): string {
    const t = s.trim().replace(/\s+/g, " ");
    return t.length > n ? t.slice(0, n - 1) + "…" : t;
}

// Friendly one-line label for the condition itself.
function condLabel(expr: string): string {
    const e = expr.trim();
    let m = e.match(/^\.([A-Za-z0-9_ -]+)$/);
    if (m) return `If ${m[1]} is set`;
    m = e.match(/^not\s+\.([A-Za-z0-9_ -]+)$/);
    if (m) return `If ${m[1]} is empty`;
    m = e.match(/^eq\s+\.([A-Za-z0-9_ -]+)\s+"([^"]*)"$/);
    if (m) return `If ${m[1]} = ${m[2]}`;
    m = e.match(/^ne\s+\.([A-Za-z0-9_ -]+)\s+"([^"]*)"$/);
    if (m) return `If ${m[1]} ≠ ${m[2]}`;
    return "Condition";
}

// Chip label shows the condition AND a short preview of each branch, so the
// if/else structure reads clearly without opening the builder.
function summarize(a: ConditionalAttrs): string {
    const then = a.thenText.trim();
    const els = a.elseText.trim();
    // A brand-new, unconfigured condition reads clearly as a call to action rather
    // than a bare "If Company is set" with no visible effect.
    if (!then && !els) return "Set up condition";
    let s = condLabel(a.expr);
    if (then) s += ` → ${clip(then, 18)}`;
    if (els) s += ` / else ${clip(els, 14)}`;
    return s;
}

function ConditionalChip({ node, updateAttributes, deleteNode, selected, editor, getPos }: NodeViewProps) {
    const attrs = node.attrs as ConditionalAttrs;
    // Open the builder once, right after insertion — not on every Edit/Preview remount.
    const [open, setOpen] = React.useState(() => consumeJustInserted(attrs.uid));
    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 6,
        maxHeight: true,
    });

    // Free raw edit of the whole {{if}}...{{end}} construct. If it re-parses into
    // the builder shape, keep it a chip; otherwise replace the node with the raw
    // literal so any hand-written Go-template conditional is possible.
    const onRaw = React.useCallback(
        (rawToken: string) => {
            const t = rawToken.trim();
            setOpen(false);
            if (!t) return;
            const parsed = parseConditionalToken(t);
            if (parsed) {
                updateAttributes(parsed);
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
        <NodeViewWrapper as="span" className="tpl-if-wrap">
            <motion.button
                ref={(el) => setReference(el)}
                type="button"
                initial={{ scale: 0.92, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                whileTap={{ scale: 0.96 }}
                transition={{ type: "spring", stiffness: 640, damping: 30 }}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => setOpen((o) => !o)}
                title={buildConditionalToken(attrs)}
                className={`tpl-if ${selected || open ? "tpl-if-active" : ""}`}
            >
                <GitBranchIcon className="h-2.5 w-2.5 shrink-0 opacity-70" />
                {summarize(attrs)}
            </motion.button>
            {typeof document !== "undefined" &&
                createPortal(
                    <AnimatePresence>
                        {open && (
                            <ConditionalBuilder
                                setFloating={setFloating}
                                floatingStyle={floatingStyle}
                                attrs={attrs}
                                onChange={(next) => updateAttributes(next)}
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

type Op = "set" | "empty" | "eq" | "ne" | "raw";

function exprToParts(expr: string): { field: string; op: Op; value: string } {
    const e = expr.trim();
    let m = e.match(/^\.([A-Za-z0-9_ -]+)$/);
    if (m) return { field: m[1], op: "set", value: "" };
    m = e.match(/^not\s+\.([A-Za-z0-9_ -]+)$/);
    if (m) return { field: m[1], op: "empty", value: "" };
    m = e.match(/^eq\s+\.([A-Za-z0-9_ -]+)\s+"([^"]*)"$/);
    if (m) return { field: m[1], op: "eq", value: m[2] };
    m = e.match(/^ne\s+\.([A-Za-z0-9_ -]+)\s+"([^"]*)"$/);
    if (m) return { field: m[1], op: "ne", value: m[2] };
    return { field: "", op: "raw", value: "" };
}

function partsToExpr(field: string, op: Op, value: string, raw: string): string {
    const f = cleanFieldName(field) || "Company";
    const v = value.replace(/"/g, "'");
    switch (op) {
        case "set":
            return `.${f}`;
        case "empty":
            return `not .${f}`;
        case "eq":
            return `eq .${f} "${v}"`;
        case "ne":
            return `ne .${f} "${v}"`;
        default:
            return raw.trim();
    }
}

const OPS: { value: Op; label: string }[] = [
    { value: "set", label: "is set" },
    { value: "empty", label: "is empty" },
    { value: "eq", label: "equals" },
    { value: "ne", label: "does not equal" },
    { value: "raw", label: "advanced…" },
];

function ConditionalBuilder({
    setFloating,
    floatingStyle,
    attrs,
    onChange,
    onRaw,
    onRemove,
    onClose,
}: {
    setFloating: (el: HTMLElement | null) => void;
    floatingStyle: React.CSSProperties;
    attrs: ConditionalAttrs;
    onChange: (next: Partial<ConditionalAttrs>) => void;
    onRaw: (rawToken: string) => void;
    onRemove: () => void;
    onClose: () => void;
}) {
    const { data: customKeys = [] } = useCustomFieldKeys();
    const localRef = React.useRef<HTMLDivElement | null>(null);
    const initial = exprToParts(attrs.expr);
    const [field, setField] = React.useState(initial.field || "Company");
    const [op, setOp] = React.useState<Op>(initial.op);
    const [value, setValue] = React.useState(initial.value);
    const [raw, setRaw] = React.useState(op === "raw" ? attrs.expr : "");
    const [thenText, setThenText] = React.useState(attrs.thenText);
    const [elseText, setElseText] = React.useState(attrs.elseText);
    const [showElse, setShowElse] = React.useState(!!attrs.elseText);
    // Full-construct raw mode: edit the entire {{if}}...{{end}} literal freely.
    const [rawMode, setRawMode] = React.useState(false);
    const [rawFull, setRawFull] = React.useState("");
    const enterRaw = () => {
        setRawFull(buildConditionalToken({ expr: partsToExpr(field, op, value, raw), thenText, elseText: showElse ? elseText : "" }));
        setRawMode(true);
    };

    const setRefs = React.useCallback(
        (el: HTMLDivElement | null) => {
            localRef.current = el;
            setFloating(el);
        },
        [setFloating],
    );

    // Push changes up as the builder is edited (skipped in raw mode, which
    // commits through onRaw instead).
    React.useEffect(() => {
        if (rawMode) return;
        onChange({ expr: partsToExpr(field, op, value, raw), thenText, elseText: showElse ? elseText : "" });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [field, op, value, raw, thenText, elseText, showElse, rawMode]);

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

    const fields = [
        ...STANDARD_VARS.map((v) => v.key),
        ...customKeys.filter((k) => !isStandardKey(k)).map((k) => cleanFieldName(k)),
    ];

    const insertVarInto = (setter: React.Dispatch<React.SetStateAction<string>>) => (token: string) => {
        setter((cur) => cur + token);
    };

    return (
        <motion.div
            ref={setRefs}
            data-floating=""
            style={floatingStyle}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.1 }}
            className="z-[60] w-[320px] overflow-hidden rounded-xl border border-slate-200 bg-white text-left shadow-[0_16px_40px_-14px_rgba(15,23,42,0.28)]"
        >
            <div className="flex items-center gap-2 border-b border-slate-100 px-3 py-2">
                <span className="flex size-6 items-center justify-center rounded-md bg-slate-100 text-slate-600">
                    <GitBranchIcon className="h-3.5 w-3.5" />
                </span>
                <span className="flex-1 text-[12.5px] font-medium text-slate-800">Condition</span>
                <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => (rawMode ? setRawMode(false) : enterRaw())}
                    className="rounded px-1.5 py-0.5 font-mono text-[10px] text-slate-400 transition-colors hover:text-sky-600"
                    title={rawMode ? "Back to the builder" : "Edit the raw template freely"}
                >
                    {rawMode ? "builder" : "{ } raw"}
                </button>
                <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={onClose}
                    className="flex size-6 items-center justify-center rounded text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600"
                    title="Done"
                >
                    <XIcon className="h-3.5 w-3.5" />
                </button>
            </div>

            <div className="space-y-2.5 p-3">
                {rawMode ? (
                    <div>
                        <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                            Raw template
                        </div>
                        <textarea
                            value={rawFull}
                            onChange={(e) => setRawFull(e.target.value)}
                            rows={5}
                            spellCheck={false}
                            className="mt-1 w-full resize-y rounded-md border border-slate-200 px-2 py-1.5 font-mono text-[11.5px] leading-relaxed text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <p className="mt-1 text-[10px] leading-snug text-slate-400">
                            The full {"{{if …}}…{{end}}"} construct. Any valid Go-template conditional works; press Apply.
                        </p>
                        <button
                            type="button"
                            onMouseDown={(e) => e.preventDefault()}
                            onClick={() => onRaw(rawFull)}
                            className="mt-1.5 h-7 rounded-md bg-slate-900 px-2.5 text-[11.5px] font-medium text-white transition-colors hover:bg-slate-800"
                        >
                            Apply raw
                        </button>
                    </div>
                ) : (
                <>
                {/* Condition */}
                <div className="space-y-1.5">
                    <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Show when</div>
                    {op !== "raw" ? (
                        <div className="flex items-center gap-1.5">
                            <Select value={field} onChange={setField} options={fields.map((f) => ({ value: f, label: f }))} />
                            <Select
                                value={op}
                                onChange={(v) => {
                                    const nv = v as Op;
                                    // Carry the current condition into the raw box so "advanced" starts
                                    // from what you already had rather than blank.
                                    if (nv === "raw" && !raw.trim()) setRaw(partsToExpr(field, op, value, ""));
                                    setOp(nv);
                                }}
                                options={OPS}
                            />
                        </div>
                    ) : (
                        <div className="flex items-center gap-1.5">
                            <span className="font-mono text-[11px] text-slate-400">{"{{if"}</span>
                            <input
                                value={raw}
                                onChange={(e) => setRaw(e.target.value)}
                                placeholder='eq .Industry "SaaS"'
                                className="h-7 min-w-0 flex-1 rounded-md border border-slate-200 px-2 font-mono text-[11.5px] text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                            />
                            <span className="font-mono text-[11px] text-slate-400">{"}}"}</span>
                            <button
                                type="button"
                                onClick={() => setOp("set")}
                                className="text-[11px] text-slate-400 hover:text-slate-600"
                                title="Back to simple"
                            >
                                simple
                            </button>
                        </div>
                    )}
                    {(op === "eq" || op === "ne") && (
                        <input
                            value={value}
                            onChange={(e) => setValue(e.target.value)}
                            placeholder="value to match"
                            className="h-7 w-full rounded-md border border-slate-200 px-2 text-[12px] text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                    )}
                </div>

                {/* Then */}
                <div className="space-y-1">
                    <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Then show</div>
                    <textarea
                        value={thenText}
                        onChange={(e) => setThenText(e.target.value)}
                        rows={2}
                        placeholder="Text shown when the condition is true"
                        className="w-full resize-y rounded-md border border-slate-200 px-2 py-1.5 text-[12.5px] leading-relaxed text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                    <VarRow fields={fields} onPick={insertVarInto(setThenText)} />
                </div>

                {/* Otherwise */}
                {showElse ? (
                    <div className="space-y-1">
                        <div className="flex items-center justify-between">
                            <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Otherwise</div>
                            <button
                                type="button"
                                onClick={() => {
                                    setShowElse(false);
                                    setElseText("");
                                }}
                                className="text-[10.5px] text-slate-400 hover:text-rose-600"
                            >
                                remove
                            </button>
                        </div>
                        <textarea
                            value={elseText}
                            onChange={(e) => setElseText(e.target.value)}
                            rows={2}
                            placeholder="Text shown otherwise"
                            className="w-full resize-y rounded-md border border-slate-200 px-2 py-1.5 text-[12.5px] leading-relaxed text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <VarRow fields={fields} onPick={insertVarInto(setElseText)} />
                    </div>
                ) : (
                    <button
                        type="button"
                        onClick={() => setShowElse(true)}
                        className="inline-flex items-center gap-1 text-[11.5px] text-slate-500 transition-colors hover:text-slate-800"
                    >
                        <PlusIcon className="h-3 w-3" /> Add an otherwise
                    </button>
                )}
                </>
                )}
            </div>

            <div className="flex items-center justify-between border-t border-slate-100 px-3 py-2">
                <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={onRemove}
                    className="inline-flex items-center gap-1.5 rounded px-1.5 py-1 text-[11.5px] text-slate-500 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <XIcon className="h-3 w-3" /> Remove
                </button>
                <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => (rawMode ? onRaw(rawFull) : onClose())}
                    className="rounded-md bg-slate-900 px-2.5 py-1 text-[11.5px] font-medium text-white transition-colors hover:bg-slate-800"
                >
                    Done
                </button>
            </div>
        </motion.div>
    );
}

function VarRow({ fields, onPick }: { fields: string[]; onPick: (token: string) => void }) {
    return (
        <div className="flex flex-wrap gap-1">
            {fields.slice(0, 6).map((f) => (
                <button
                    key={f}
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => onPick(buildToken(f))}
                    title={`Insert ${buildToken(f)}`}
                    className="rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 font-mono text-[10px] text-slate-500 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                >
                    {f}
                </button>
            ))}
        </div>
    );
}

// Small native-free select styled to the theme.
function Select<T extends string>({
    value,
    onChange,
    options,
}: {
    value: T;
    onChange: (v: T) => void;
    options: { value: T; label: string }[];
}) {
    return (
        <div className="relative min-w-0 flex-1">
            <select
                value={value}
                onChange={(e) => onChange(e.target.value as T)}
                className="h-7 w-full appearance-none rounded-md border border-slate-200 bg-white pl-2 pr-6 text-[12px] text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
            >
                {options.map((o) => (
                    <option key={o.value} value={o.value}>
                        {o.label}
                    </option>
                ))}
            </select>
            <ChevronDownIcon className="pointer-events-none absolute right-1.5 top-1/2 h-3 w-3 -translate-y-1/2 text-slate-400" />
        </div>
    );
}
