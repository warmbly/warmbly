// EditorSuggest — a caret type-ahead for the body/instruction editors. Typing
// `{{` opens a filtered inserter at the caret with three groups: merge Fields
// (insert a variable chip), Logic (an if/else condition chip), and Functions
// (Go-template helpers like default/title/upper). Anchored to a floating-ui
// virtual caret so it follows the caret and stays glued through scroll. Driven
// per editor (not a global plugin) so several editors can share a page.

import React from "react";
import { createPortal } from "react-dom";
import type { Editor } from "@tiptap/react";
import { AnimatePresence, motion } from "framer-motion";
import { BracesIcon, GitBranchIcon, FunctionSquareIcon } from "lucide-react";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import { STANDARD_VARS, buildToken, cleanFieldName, isStandardKey } from "@/lib/templateVars";
import { useAnchoredFloating, caretReference } from "@/hooks/useAnchoredFloating";

type Group = "Fields" | "Logic" | "Functions";

// How picking an item mutates the doc, after the typed `{{…` trigger is removed.
type Insert =
    | { type: "chip"; token: string } // a {{.Field}} (optionally with a helper) chip
    | { type: "conditional" } // an {{if}}/{{else}} condition chip
    | { type: "text"; text: string }; // a raw template snippet (advanced functions)

interface Item {
    id: string;
    group: Group;
    label: string;
    hint: string; // shown as muted code on the right
    search: string; // lowercased haystack for filtering
    insert: Insert;
}

const GROUP_ICON: Record<Group, typeof BracesIcon> = {
    Fields: BracesIcon,
    Logic: GitBranchIcon,
    Functions: FunctionSquareIcon,
};

// The fixed Logic + Functions helpers. Function snippets carry a representative
// field the user can edit; `default` stays a chip because the variable chip
// already models a fallback.
const HELPERS: Item[] = [
    {
        id: "if",
        group: "Logic",
        label: "If / else condition",
        hint: "{{if}}",
        search: "if else condition when show only",
        insert: { type: "conditional" },
    },
    {
        id: "default",
        group: "Functions",
        label: "Fallback if empty",
        hint: '| default',
        search: "default fallback empty missing",
        insert: { type: "chip", token: '{{.FirstName | default "there"}}' },
    },
    {
        id: "title",
        group: "Functions",
        label: "Title case",
        hint: "| title",
        search: "title case capitalize proper",
        insert: { type: "text", text: "{{.FirstName | title}}" },
    },
    {
        id: "upper",
        group: "Functions",
        label: "Uppercase",
        hint: "| upper",
        search: "upper uppercase caps",
        insert: { type: "text", text: "{{.Company | upper}}" },
    },
    {
        id: "lower",
        group: "Functions",
        label: "Lowercase",
        hint: "| lower",
        search: "lower lowercase",
        insert: { type: "text", text: "{{.Email | lower}}" },
    },
    {
        id: "trim",
        group: "Functions",
        label: "Trim spaces",
        hint: "| trim",
        search: "trim whitespace spaces clean",
        insert: { type: "text", text: "{{.Company | trim}}" },
    },
];

export default function EditorSuggest({ editor }: { editor: Editor }) {
    const { data: customKeys = [] } = useCustomFieldKeys();
    const [trigger, setTrigger] = React.useState<{ from: number; query: string } | null>(null);
    const [active, setActive] = React.useState(0);

    const candidates = React.useMemo<Item[]>(() => {
        const fields: Item[] = [
            ...STANDARD_VARS.map((v) => ({
                id: `f:${v.key}`,
                group: "Fields" as const,
                label: v.label,
                hint: v.token,
                search: `${v.key} ${v.label}`.toLowerCase(),
                insert: { type: "chip" as const, token: v.token },
            })),
            ...customKeys
                .filter((k) => !isStandardKey(k))
                .map((k) => ({
                    id: `f:${k}`,
                    group: "Fields" as const,
                    label: k,
                    hint: buildToken(k),
                    search: `${cleanFieldName(k)} ${k}`.toLowerCase(),
                    insert: { type: "chip" as const, token: buildToken(k) },
                })),
        ];
        return [...fields, ...HELPERS];
    }, [customKeys]);

    const items = React.useMemo<Item[]>(() => {
        if (!trigger) return [];
        const q = trigger.query.trim().toLowerCase();
        const filtered = q ? candidates.filter((c) => c.search.includes(q)) : candidates;
        return filtered.slice(0, 10);
    }, [trigger, candidates]);

    const open = !!trigger && items.length > 0;
    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 6,
        maxHeight: true,
    });

    // Recompute the trigger from the caret on every edit/selection change.
    React.useEffect(() => {
        const recompute = () => {
            const sel = editor.state.selection;
            if (!sel.empty) {
                setTrigger(null);
                return;
            }
            const $from = sel.$from;
            const before = $from.parent.textBetween(0, $from.parentOffset, "￼", "￼");
            const m = before.match(/\{\{\s*\.?([A-Za-z0-9_ ]*)$/);
            if (!m) {
                setTrigger(null);
                return;
            }
            setActive(0);
            setTrigger({ from: $from.start() + (m.index ?? 0), query: m[1] ?? "" });
        };
        editor.on("update", recompute);
        editor.on("selectionUpdate", recompute);
        return () => {
            editor.off("update", recompute);
            editor.off("selectionUpdate", recompute);
        };
    }, [editor]);

    // Point floating-ui at a virtual caret element; refresh it whenever the caret
    // moves (a new object identity re-runs the positioning effect).
    React.useEffect(() => {
        if (!trigger) {
            setReference(null);
            return;
        }
        setReference(
            caretReference(() => {
                try {
                    const c = editor.view.coordsAtPos(editor.state.selection.from);
                    return new DOMRect(c.left, c.top, 0, c.bottom - c.top);
                } catch {
                    return null;
                }
            }, editor.view.dom),
        );
    }, [trigger, editor, setReference]);

    const select = React.useCallback(
        (item: Item) => {
            if (!trigger) return;
            const chain = editor
                .chain()
                .focus()
                .deleteRange({ from: trigger.from, to: editor.state.selection.from });
            if (item.insert.type === "chip") {
                chain.insertVariable(item.insert.token).run();
            } else if (item.insert.type === "conditional") {
                chain.insertConditional().run();
            } else {
                chain.insertContent(item.insert.text).run();
            }
            setTrigger(null);
        },
        [editor, trigger],
    );

    // Keyboard nav while open, in capture so the editor doesn't also act on it.
    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "ArrowDown") {
                e.preventDefault();
                setActive((a) => (a + 1) % items.length);
            } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setActive((a) => (a - 1 + items.length) % items.length);
            } else if (e.key === "Enter" || e.key === "Tab") {
                e.preventDefault();
                e.stopPropagation();
                select(items[Math.min(active, items.length - 1)]);
            } else if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                setTrigger(null);
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, [open, items, active, select]);

    if (typeof document === "undefined") return null;

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    ref={setFloating}
                    data-floating=""
                    style={floatingStyle}
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.1 }}
                    className="z-[60] w-64 overflow-y-auto rounded-lg border border-slate-200 bg-white py-1 shadow-[0_10px_30px_-10px_rgba(15,23,42,0.25)]"
                >
                    {items.map((item, i) => {
                        const Icon = GROUP_ICON[item.group];
                        const showHeader = i === 0 || items[i - 1].group !== item.group;
                        return (
                            <React.Fragment key={item.id}>
                                {showHeader && (
                                    <div className="px-2.5 pb-0.5 pt-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                        {item.group}
                                    </div>
                                )}
                                <button
                                    type="button"
                                    onMouseDown={(e) => {
                                        e.preventDefault();
                                        select(item);
                                    }}
                                    onMouseEnter={() => setActive(i)}
                                    className={`flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors ${
                                        i === active ? "bg-sky-50" : "hover:bg-slate-50"
                                    }`}
                                >
                                    <Icon className={`h-3 w-3 shrink-0 ${i === active ? "text-sky-500" : "text-slate-400"}`} />
                                    <span className="min-w-0 flex-1 truncate text-[12.5px] text-slate-700">{item.label}</span>
                                    <code className="shrink-0 font-mono text-[10px] text-slate-400">{item.hint}</code>
                                </button>
                            </React.Fragment>
                        );
                    })}
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}
