// Rich email-body editor for campaign Steps, built on TipTap (no deprecated
// execCommand). Controlled by an HTML string; emits HTML on change. Ships a
// house-theme toolbar (headings, bold/italic/underline/strike, lists, link), a
// one-click {{variable}} inserter, and a spintax `{a|b}` helper. Personalization
// tokens are just text, so they survive serialization untouched.

import React from "react";
import { useEditor, EditorContent, type Editor } from "@tiptap/react";
import Document from "@tiptap/extension-document";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import Bold from "@tiptap/extension-bold";
import Italic from "@tiptap/extension-italic";
import Underline from "@tiptap/extension-underline";
import Strike from "@tiptap/extension-strike";
import Heading from "@tiptap/extension-heading";
import Link from "@tiptap/extension-link";
import { BulletList, OrderedList, ListItem } from "@tiptap/extension-list";
import {
    BoldIcon,
    ItalicIcon,
    UnderlineIcon,
    StrikethroughIcon,
    Heading2Icon,
    ListIcon,
    ListOrderedIcon,
    Link2Icon,
    BracesIcon,
    ShuffleIcon,
    CheckIcon,
    XIcon,
    ChevronDownIcon,
} from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import useClickOutside from "@/hooks/useClickOutside";
import { WEBSITE_URL } from "@/lib/information";

export default function RichTextEditor({
    html,
    onChange,
    variables,
    placeholder,
}: {
    html: string;
    onChange: (html: string) => void;
    variables: string[];
    placeholder?: string;
}) {
    const editor = useEditor({
        extensions: [
            Document,
            Paragraph,
            Text,
            Bold,
            Italic,
            Underline,
            Strike,
            Heading.configure({ levels: [2, 3] }),
            BulletList,
            OrderedList,
            ListItem,
            Link.configure({ openOnClick: false, autolink: true }),
        ],
        content: html || "",
        editorProps: {
            attributes: {
                class: "tiptap-body min-h-[260px] px-3 py-2.5 text-[13px] leading-relaxed text-slate-800 focus:outline-none",
            },
        },
        onUpdate: ({ editor }) => onChange(editor.getHTML()),
    });

    // Keep the editor in sync when the value changes from outside (template
    // applied, step switched, reset) without clobbering the user's caret on
    // their own edits.
    React.useEffect(() => {
        if (!editor) return;
        const current = editor.getHTML();
        if (html !== current) {
            editor.commands.setContent(html || "", { emitUpdate: false });
        }
    }, [html, editor]);

    if (!editor) return null;

    return (
        <div className="rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
            <Toolbar editor={editor} variables={variables} />
            <div className="relative">
                <EditorContent editor={editor} />
                {placeholder && editor.isEmpty && (
                    <p className="pointer-events-none absolute left-3 top-2.5 text-[13px] text-slate-300 select-none">
                        {placeholder}
                    </p>
                )}
            </div>
        </div>
    );
}

function Toolbar({ editor, variables }: { editor: Editor; variables: string[] }) {
    const [linkOpen, setLinkOpen] = React.useState(false);
    const [linkUrl, setLinkUrl] = React.useState("");

    const applyLink = () => {
        const url = linkUrl.trim();
        if (url) {
            editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run();
        } else {
            editor.chain().focus().unsetLink().run();
        }
        setLinkOpen(false);
        setLinkUrl("");
    };

    return (
        <div className="relative flex flex-wrap items-center gap-0.5 border-b border-slate-200/70 px-1.5 py-1">
            <Btn active={editor.isActive("bold")} onClick={() => editor.chain().focus().toggleBold().run()} title="Bold">
                <BoldIcon className="w-3.5 h-3.5" />
            </Btn>
            <Btn active={editor.isActive("italic")} onClick={() => editor.chain().focus().toggleItalic().run()} title="Italic">
                <ItalicIcon className="w-3.5 h-3.5" />
            </Btn>
            <Btn active={editor.isActive("underline")} onClick={() => editor.chain().focus().toggleUnderline().run()} title="Underline">
                <UnderlineIcon className="w-3.5 h-3.5" />
            </Btn>
            <Btn active={editor.isActive("strike")} onClick={() => editor.chain().focus().toggleStrike().run()} title="Strikethrough">
                <StrikethroughIcon className="w-3.5 h-3.5" />
            </Btn>
            <Divider />
            <Btn
                active={editor.isActive("heading", { level: 2 })}
                onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}
                title="Heading"
            >
                <Heading2Icon className="w-3.5 h-3.5" />
            </Btn>
            <Btn active={editor.isActive("bulletList")} onClick={() => editor.chain().focus().toggleBulletList().run()} title="Bullet list">
                <ListIcon className="w-3.5 h-3.5" />
            </Btn>
            <Btn active={editor.isActive("orderedList")} onClick={() => editor.chain().focus().toggleOrderedList().run()} title="Numbered list">
                <ListOrderedIcon className="w-3.5 h-3.5" />
            </Btn>
            <Btn
                active={editor.isActive("link")}
                onClick={() => {
                    setLinkUrl(editor.getAttributes("link").href ?? "");
                    setLinkOpen((o) => !o);
                }}
                title="Link"
            >
                <Link2Icon className="w-3.5 h-3.5" />
            </Btn>
            <Divider />
            <VariableMenu onPick={(v) => editor.chain().focus().insertContent(v).run()} variables={variables} />
            <Btn
                onClick={() => editor.chain().focus().insertContent("{option one|option two}").run()}
                title="Insert spintax — randomly picks one option per send"
            >
                <ShuffleIcon className="w-3.5 h-3.5" />
            </Btn>

            <AnimatePresence>
                {linkOpen && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute left-1.5 top-full z-20 mt-1 flex items-center gap-1 rounded-md border border-slate-200 bg-white p-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]"
                    >
                        <input
                            autoFocus
                            value={linkUrl}
                            onChange={(e) => setLinkUrl(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                    e.preventDefault();
                                    applyLink();
                                } else if (e.key === "Escape") {
                                    setLinkOpen(false);
                                }
                            }}
                            placeholder="https://…"
                            className="h-7 w-56 rounded border border-slate-200 px-2 text-[12px] text-slate-800 outline-none focus:border-sky-400"
                        />
                        <button
                            type="button"
                            onClick={applyLink}
                            className="size-7 inline-flex items-center justify-center rounded text-emerald-600 hover:bg-emerald-50"
                            title="Apply"
                        >
                            <CheckIcon className="w-3.5 h-3.5" />
                        </button>
                        <button
                            type="button"
                            onClick={() => setLinkOpen(false)}
                            className="size-7 inline-flex items-center justify-center rounded text-slate-400 hover:bg-slate-100"
                            title="Cancel"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function Btn({
    active,
    onClick,
    title,
    children,
}: {
    active?: boolean;
    onClick: () => void;
    title: string;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            title={title}
            aria-pressed={active}
            onMouseDown={(e) => e.preventDefault()}
            onClick={onClick}
            className={`size-7 inline-flex items-center justify-center rounded transition-colors ${
                active ? "bg-sky-50 text-sky-700" : "text-slate-500 hover:text-slate-900 hover:bg-slate-100"
            }`}
        >
            {children}
        </button>
    );
}

function Divider() {
    return <span className="mx-0.5 h-4 w-px bg-slate-200" />;
}

// Friendly labels + one-line descriptions for the standard contact tokens, so
// the menu explains what each value does rather than dumping raw {{.Tokens}}.
const TOKEN_META: Record<string, { label: string; desc: string }> = {
    "{{.FirstName}}": { label: "First name", desc: "The contact's first name" },
    "{{.LastName}}": { label: "Last name", desc: "The contact's last name" },
    "{{.Email}}": { label: "Email", desc: "The contact's email address" },
    "{{.Company}}": { label: "Company", desc: "Where the contact works" },
    "{{.Phone}}": { label: "Phone", desc: "The contact's phone number" },
};

// cleanFieldName strips braces/leading dots so a pasted "{{.role}}" or ".role"
// still resolves to the bare key.
function cleanFieldName(raw: string): string {
    return raw.replace(/[{}]/g, "").replace(/^\.+/, "").trim();
}

// Shared variable inserter — a personalization menu that explains each field and
// can insert a custom field by name. Flips horizontally so it never overflows
// the editor edge.
export function VariableMenu({ onPick, variables }: { onPick: (token: string) => void; variables: string[] }) {
    const [open, setOpen] = React.useState(false);
    // Fixed-viewport coordinates computed from the trigger, so the panel escapes
    // the editor's overflow-hidden clipping and is clamped fully on-screen.
    const [pos, setPos] = React.useState<{ left: number; width: number; top?: number; bottom?: number } | null>(null);
    const [custom, setCustom] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(ref, () => setOpen(false));

    const toggle = () => {
        if (!open && triggerRef.current) {
            const r = triggerRef.current.getBoundingClientRect();
            const margin = 12;
            const vw = window.innerWidth;
            const vh = window.innerHeight;
            const width = Math.min(vw < 640 ? vw - margin * 2 : 352, vw - margin * 2);
            // Clamp horizontally so the full width is always on-screen.
            const left = Math.max(margin, Math.min(r.left, vw - width - margin));
            // Open upward when the trigger sits low in the viewport.
            const up = r.bottom > vh * 0.55;
            setPos(up ? { left, width, bottom: vh - r.top + 6 } : { left, width, top: r.bottom + 6 });
        }
        setOpen((o) => !o);
    };

    const customName = cleanFieldName(custom);
    const insertCustom = () => {
        if (!customName) return;
        onPick(`{{.${customName}}}`);
        setCustom("");
        setOpen(false);
    };

    return (
        <div ref={ref} className="relative">
            <button
                ref={triggerRef}
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={toggle}
                title="Insert a personalization variable"
                className="h-7 px-1.5 inline-flex items-center gap-1 rounded text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
            >
                <BracesIcon className="w-3.5 h-3.5" />
                <ChevronDownIcon className="w-3 h-3" />
            </button>
            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: pos?.bottom !== undefined ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: pos?.bottom !== undefined ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        style={
                            pos
                                ? {
                                      left: pos.left,
                                      width: pos.width,
                                      ...(pos.top !== undefined ? { top: pos.top } : { bottom: pos.bottom }),
                                  }
                                : undefined
                        }
                        className="fixed z-40 max-h-[76vh] overflow-y-auto rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] sm:max-h-[80vh]"
                    >
                        <div className="px-3 py-2 border-b border-slate-100">
                            <p className="text-[12px] font-medium text-slate-800">Personalization</p>
                            <p className="text-[10.5px] text-slate-400 mt-0.5">
                                Replaced per contact on send · click to insert · hover for what each does
                            </p>
                        </div>

                        {/* Contact fields — compact 2-column grid (description on hover). */}
                        <div className="px-2 pt-2">
                            <div className="px-1 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                Contact fields
                            </div>
                            <div className="grid grid-cols-2 gap-1">
                                {variables.map((v) => {
                                    const meta = TOKEN_META[v];
                                    return (
                                        <button
                                            key={v}
                                            type="button"
                                            title={meta?.desc}
                                            onMouseDown={(e) => e.preventDefault()}
                                            onClick={() => {
                                                onPick(v);
                                                setOpen(false);
                                            }}
                                            className="flex min-w-0 flex-col items-start rounded-md border border-slate-200 px-2 py-1 text-left transition-colors hover:border-sky-300 hover:bg-sky-50/50"
                                        >
                                            <span className="w-full truncate text-[11.5px] text-slate-700">
                                                {meta?.label ?? v}
                                            </span>
                                            <code className="w-full truncate font-mono text-[9.5px] text-slate-400">
                                                {v}
                                            </code>
                                        </button>
                                    );
                                })}
                            </div>
                        </div>

                        <div className="px-3 pt-2.5 pb-2">
                            <div className="px-0 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                Custom field
                            </div>
                            <div className="flex items-center gap-1.5">
                                <input
                                    value={custom}
                                    onChange={(e) => setCustom(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter") {
                                            e.preventDefault();
                                            insertCustom();
                                        }
                                    }}
                                    placeholder="field name (e.g. role)"
                                    className="h-7 min-w-0 flex-1 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                />
                                <button
                                    type="button"
                                    onMouseDown={(e) => e.preventDefault()}
                                    onClick={insertCustom}
                                    disabled={!customName}
                                    className="h-7 shrink-0 rounded-md bg-sky-600 px-2.5 text-[11.5px] font-medium text-white transition-colors hover:bg-sky-700 disabled:opacity-50"
                                >
                                    Insert
                                </button>
                            </div>
                            <p className="mt-1 text-[10px] text-slate-400">
                                Inserts <code className="font-mono text-slate-500">{`{{.${customName || "name"}}}`}</code>{" "}
                                — exact field name; blank if the contact lacks it.
                            </p>
                        </div>

                        {/* Conditionals — compact 2-column snippet grid. */}
                        <div className="px-2 pb-2 pt-1.5 border-t border-slate-100">
                            <div className="px-1 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                Conditionals
                            </div>
                            <div className="grid grid-cols-2 gap-1">
                                {[
                                    { label: "If set", code: "{{if .Company}}{{end}}" },
                                    { label: "If / else", code: "{{if .Company}}{{else}}{{end}}" },
                                    { label: "If equals", code: '{{if eq .Company "Acme"}}{{end}}' },
                                    { label: "Fallback", code: '{{.FirstName | default "there"}}' },
                                ].map((s) => (
                                    <button
                                        key={s.label}
                                        type="button"
                                        title={s.code}
                                        onMouseDown={(e) => e.preventDefault()}
                                        onClick={() => {
                                            onPick(s.code);
                                            setOpen(false);
                                        }}
                                        className="flex min-w-0 flex-col items-start rounded-md border border-slate-200 px-2 py-1 text-left transition-colors hover:border-sky-300 hover:bg-sky-50/50"
                                    >
                                        <span className="w-full truncate text-[11.5px] text-slate-700">{s.label}</span>
                                        <code className="w-full truncate font-mono text-[9.5px] text-slate-400">
                                            {s.code}
                                        </code>
                                    </button>
                                ))}
                            </div>
                            <p className="px-1 pt-1.5 text-[10px] text-slate-400">
                                Type text between the tags. Every <code className="font-mono">{"{{if}}"}</code> needs an{" "}
                                <code className="font-mono">{"{{end}}"}</code>; missing fields count as empty. Use{" "}
                                <code className="font-mono">| default &quot;…&quot;</code> for a fallback when a field is blank.
                            </p>
                        </div>

                        <a
                            href={`${WEBSITE_URL}/learn/personalization`}
                            target="_blank"
                            rel="noreferrer"
                            onMouseDown={(e) => e.preventDefault()}
                            className="flex items-center justify-between gap-2 border-t border-slate-100 px-3 py-2 text-[11.5px] font-medium text-sky-600 transition-colors hover:bg-sky-50/60"
                        >
                            Full guide &amp; examples
                            <span aria-hidden="true">↗</span>
                        </a>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
