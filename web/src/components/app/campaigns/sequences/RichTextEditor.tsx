// Rich email-body editor for campaign Steps, built on TipTap (no deprecated
// execCommand). Controlled by an HTML string; emits HTML on change. Ships a
// house-theme toolbar (undo/redo, headings, bold/italic/underline/strike,
// lists, link, images, call-to-action buttons), a one-click {{variable}}
// inserter, a spintax `{a|b}` helper, and an HTML source view. Personalization
// tokens are just text, so they survive serialization untouched.
//
// Paste is normalised on the way in (pasteHtml.ts): a message copied out of
// Gmail, Outlook or Word brings its own blank-line scaffolding, which our own
// paragraph margins would then render a second time. A paste that is a whole
// HTML email is not normalised at all: it switches the body to HTML mode and
// is kept exactly as written (pastedEmail.ts), because no schema can hold a
// document with its own <head> and <style>.
//
// HTML mode is persisted on the step (body_code), not local state. The visual
// editor never parses a body that is in HTML mode, so reopening a step written
// as markup shows the markup, instead of the gutted version the schema would
// have made of it and then saved on the next keystroke (issue #393).

import React from "react";
import { createPortal } from "react-dom";
import { useEditor, EditorContent, type Editor } from "@tiptap/react";
import Document from "@tiptap/extension-document";
import Text from "@tiptap/extension-text";
import Bold from "@tiptap/extension-bold";
import Italic from "@tiptap/extension-italic";
import Underline from "@tiptap/extension-underline";
import Strike from "@tiptap/extension-strike";
import Heading from "@tiptap/extension-heading";
import Link from "@tiptap/extension-link";
import HardBreak from "@tiptap/extension-hard-break";
import { UndoRedo } from "@tiptap/extensions";
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
    ClipboardListIcon,
    CheckIcon,
    XIcon,
    ChevronDownIcon,
    SparklesIcon,
    GitBranchIcon,
    Undo2Icon,
    Redo2Icon,
    CodeIcon,
    PencilLineIcon,
} from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import useClickOutside from "@/hooks/useClickOutside";
import { useAnchoredFloating } from "@/hooks/useAnchoredFloating";
import { useConfirm } from "@/hooks/context/confirm";
import RichTextAIEdit from "@/components/app/ai/RichTextAIEdit";
import RichTextAICaret from "@/components/app/ai/RichTextAICaret";
import { useForms } from "@/lib/api/hooks/app/forms";
import { EmailImage } from "./nodes/EmailImageNode";
import { EmailButton } from "./nodes/EmailButtonNode";
import { ImageBubble, ImageMenu } from "./ImageControls";
import { ButtonBubble, ButtonInsert } from "./ButtonControls";
import { AlignMenu, ColorMenu, TableMenu, TypeMenu } from "./DesignControls";
import { insertImage, isSupportedImageFile, useImageUpload } from "./imageUpload";
import { normalizePastedHTML } from "./pasteHtml";
import { detectPastedEmail } from "@/lib/email/pastedEmail";
import { emailDesignExtensions, EmailParagraph } from "./nodes/emailHtml";
import { VariableNode } from "./nodes/VariableNode";
import { AIVariableNode } from "./nodes/AIVariableNode";
import { ConditionalNode } from "./nodes/ConditionalNode";
import { FormLinkNode } from "./nodes/FormLinkNode";
import EditorSuggest from "./nodes/EditorSuggest";
import {
    TOKEN_META,
    UNSUBSCRIBE_TOKEN,
    cleanFieldName,
    parseToken,
    buildToken,
    isStandardKey,
    upgradeVariableTokens,
} from "@/lib/templateVars";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";

// A field token ({{.Company}}, or with a default fallback) becomes an atomic
// chip; conditionals/spintax/other snippets insert as plain text.
function insertToken(editor: Editor, token: string) {
    if (parseToken(token)) {
        editor.chain().focus().insertVariable(token).run();
    } else {
        editor.chain().focus().insertContent(token).run();
    }
}

// Picking the unsubscribe link with text selected links that text instead of
// dropping a chip, so the copy keeps its own wording. With no selection the
// chip is inserted and the send path gives it an anchor of its own.
function insertLinkToken(editor: Editor, token: string) {
    if (token !== UNSUBSCRIBE_TOKEN || editor.state.selection.empty) {
        insertToken(editor, token);
        return;
    }
    editor.chain().focus().setLink({ href: token }).run();
}

export default function RichTextEditor({
    html,
    onChange,
    code = false,
    onCodeChange,
    variables,
    links = [],
    placeholder,
    minimal = false,
}: {
    html: string;
    onChange: (html: string) => void;
    // HTML mode, persisted on the step as body_code. While it is on the raw
    // markup IS the body and the visual editor never parses it, so a designed
    // email survives being reopened.
    code?: boolean;
    onCodeChange?: (code: boolean) => void;
    variables: string[];
    // Per-send link tokens offered in the variable menu (body editors only).
    links?: string[];
    placeholder?: string;
    // Compact mode for embedding in a small surface (e.g. the AI-block prompt):
    // an insert-only toolbar (variables + condition + spintax), no text
    // formatting, no AI carets, and a short body. Chips, the {{ type-ahead, and
    // conditionals behave exactly as in the full editor.
    minimal?: boolean;
}) {
    const confirm = useConfirm();
    // The editor is created once, so anything a ProseMirror handler needs at
    // paste/drop time is reached through a ref rather than a closure over the
    // render that created it.
    const editorRef = React.useRef<Editor | null>(null);
    const { run: uploadImage } = useImageUpload();
    const uploadRef = React.useRef(uploadImage);
    uploadRef.current = uploadImage;
    const minimalRef = React.useRef(minimal);
    minimalRef.current = minimal;
    // Declared with the other paste-time refs, above the editor that closes
    // over it: a const referenced before its declaration runs would throw, and
    // only the fact that a paste happens after render keeps that hypothetical.
    const adoptRef = React.useRef<((markup: string) => void) | null>(null);

    // Uploads an image file dropped or pasted into the body and places it,
    // optionally at a document position (where it was dropped).
    const placeImageFiles = React.useCallback(async (files: File[], at?: number) => {
        if (!editorRef.current) return;
        for (const file of files) {
            const created = await uploadRef.current(file);
            if (!created || !editorRef.current) continue;
            if (typeof at === "number") editorRef.current.commands.setTextSelection(at);
            insertImage(editorRef.current, { url: created.url, alt: created.filename });
        }
    }, []);
    const placeRef = React.useRef(placeImageFiles);
    placeRef.current = placeImageFiles;

    const editor = useEditor({
        extensions: [
            Document,
            EmailParagraph,
            Text,
            Bold,
            Italic,
            Underline,
            Strike,
            HardBreak,
            Heading.configure({ levels: [2, 3] }),
            BulletList,
            OrderedList,
            ListItem,
            Link.configure({ openOnClick: false, autolink: true }),
            EmailImage,
            EmailButton,
            // Real email markup: table layout, <div> containers, colours,
            // fonts and alignment. Without these a pasted design keeps its
            // words and loses everything that made it a design.
            ...emailDesignExtensions,
            // Without this there is no undo stack at all: Ctrl+Z fell through
            // to the browser, which cannot undo a ProseMirror transaction.
            UndoRedo,
            VariableNode,
            AIVariableNode,
            ConditionalNode,
            FormLinkNode,
        ],
        content: upgradeVariableTokens(html || ""),
        // Toolbar state (active marks, undo availability, the selected image)
        // is read from the editor during render, so it has to repaint on a
        // caret move, not only on a keystroke.
        shouldRerenderOnTransaction: true,
        editorProps: {
            attributes: {
                class: `tiptap-body ${
                    minimal ? "min-h-[68px] text-[13px]" : "min-h-[260px] px-3 py-2.5 text-[13px]"
                } leading-relaxed text-slate-800 focus:outline-none`,
            },
            transformPastedHTML: (pasted) => normalizePastedHTML(pasted),
            handleDOMEvents: {
                // An <a> this editor renders itself — the wrapper around a
                // linked image, the anchor inside a button — sits in a node
                // that is not contenteditable, so a plain click follows it and
                // the dashboard navigates away mid-edit. Ctrl/Cmd still opens
                // it, which is how a link is opened from an editor anywhere.
                click: (_view, event) => {
                    const target = event.target as HTMLElement | null;
                    if (!event.metaKey && !event.ctrlKey && target?.closest?.("a[href]")) {
                        event.preventDefault();
                    }
                    return false;
                },
            },
            handlePaste: (_view, event) => {
                if (minimalRef.current) return false;
                const files = Array.from(event.clipboardData?.files ?? []).filter(isSupportedImageFile);
                if (files.length > 0) {
                    event.preventDefault();
                    void placeRef.current(files);
                    return true;
                }
                // A whole HTML email is kept byte for byte instead of being
                // parsed into the schema, which would drop its <style> block
                // and its <head> without saying so.
                const document_ = detectPastedEmail(event.clipboardData);
                if (document_ && adoptRef.current) {
                    event.preventDefault();
                    adoptRef.current(document_);
                    return true;
                }
                return false;
            },
            handleDrop: (view, event, _slice, moved) => {
                // `moved` is the editor's own content being dragged inside it.
                if (minimalRef.current || moved) return false;
                const dt = event instanceof DragEvent ? event.dataTransfer : null;
                const files = Array.from(dt?.files ?? []).filter(isSupportedImageFile);
                if (files.length === 0) return false;
                event.preventDefault();
                const at = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos;
                void placeRef.current(files, at);
                return true;
            },
        },
        onUpdate: ({ editor }) => onChange(editor.getHTML()),
    });
    editorRef.current = editor;

    // HTML mode is the step's own persisted state, so a body written as markup
    // is still markup when the step is reopened. While it is on, the textarea
    // holds the body and the editor is not the source of truth.
    adoptRef.current = onCodeChange
        ? (markup: string) => {
              onChange(prettyHTML(markup));
              onCodeChange(true);
              toast.success("Kept as HTML. Use the toolbar's Visual button to edit it as rich text.");
          }
        : null;

    // Keep the editor in sync when the value changes from outside (template
    // applied, step switched, reset) without clobbering the user's caret on
    // their own edits. In HTML mode there is nothing to sync: parsing the body
    // is exactly what that mode exists to prevent.
    React.useEffect(() => {
        if (!editor || code) return;
        const current = editor.getHTML();
        const incoming = upgradeVariableTokens(html || "");
        if (incoming !== current) {
            editor.commands.setContent(incoming, { emitUpdate: false });
        }
    }, [html, editor, code]);

    // Switching modes. Into HTML is lossless; out of it hands the markup to
    // the schema, which keeps only what it can represent, so anything it would
    // drop is named while undoing the switch is still one click. The parsed
    // result is committed rather than left to the next keystroke: what the
    // editor shows after the switch has to be what the step will send.
    const toggleCode = () => {
        if (!editor || !onCodeChange) return;
        if (!code) {
            onChange(prettyHTML(editor.getHTML()));
            onCodeChange(true);
            return;
        }
        const apply = () => {
            editor.commands.setContent(upgradeVariableTokens(html || ""), { emitUpdate: true });
            onCodeChange(false);
        };
        const dropped = unsupportedTags(html || "");
        if (dropped.length > 0) {
            confirm.show(
                `The visual editor cannot hold ${dropped.map((t) => `<${t}>`).join(", ")}. ` +
                    "Switching removes those tags and keeps the text inside them. Stay in HTML to keep them.",
                apply,
            );
            return;
        }
        apply();
    };

    if (!editor) return null;

    // Bare mode: no toolbar, no border box — a plain, borderless writing surface
    // (the small AI-block instruction). Merge tokens still render as chips and the
    // {{ type-ahead still works; formatting and AI carets are omitted. A soft sky
    // underline appears on focus, nothing more.
    if (minimal) {
        return (
            <div className="border-b border-transparent pb-1 transition-colors focus-within:border-sky-300">
                <div className="relative">
                    <EditorContent editor={editor} />
                    {placeholder && editor.isEmpty && (
                        <p className="pointer-events-none absolute left-0 top-0 select-none text-[13px] text-slate-300">
                            {placeholder}
                        </p>
                    )}
                </div>
                {/* Type `{{` → variable type-ahead at the caret. */}
                <EditorSuggest editor={editor} links={links} />
            </div>
        );
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
            <Toolbar
                editor={editor}
                variables={variables}
                links={links}
                sourceOpen={code}
                onToggleSource={onCodeChange ? toggleCode : undefined}
            />
            {code ? (
                <HTMLSource value={html} onChange={onChange} />
            ) : (
                <div className="relative">
                    <EditorContent editor={editor} />
                    {placeholder && editor.isEmpty && (
                        <p className="pointer-events-none absolute left-3 top-2.5 text-[13px] text-slate-300 select-none">
                            {placeholder}
                        </p>
                    )}
                </div>
            )}
            {!code && (
                <>
                    {/* Select an image → size, alignment, alt text and link over it. */}
                    <ImageBubble editor={editor} />
                    {/* Select a button → its text, link, colour, size and shape. */}
                    <ButtonBubble editor={editor} />
                    {/* Select text → floating "Edit with AI" pill over the selection. */}
                    <RichTextAIEdit editor={editor} />
                    {/* Collapsed caret → sparkle companion + ⌘J to write with AI. */}
                    <RichTextAICaret editor={editor} />
                    {/* Type `{{` → variable type-ahead at the caret. */}
                    <EditorSuggest editor={editor} links={links} />
                </>
            )}
        </div>
    );
}

// HTMLSource is the raw-markup view. It holds the body itself, not a copy, so
// what the user types here is byte for byte what the step sends.
function HTMLSource({ value, onChange }: { value: string; onChange: (v: string) => void }) {
    return (
        <div>
            <textarea
                value={value}
                onChange={(e) => onChange(e.target.value)}
                spellCheck={false}
                placeholder="<p>Hi {{.FirstName}}, …</p>"
                className="min-h-[260px] w-full resize-y bg-white px-3 py-2.5 font-mono text-[12px] leading-relaxed text-slate-800 outline-none"
            />
            <p className="border-t border-slate-200/70 px-3 py-1.5 text-[10.5px] text-slate-400">
                This is what the step sends. Merge fields, conditions and spintax all still work here.
            </p>
        </div>
    );
}

// prettyHTML puts each block on its own line so the source view is readable.
// The break only ever goes BETWEEN blocks, never inside one: whitespace there
// is not content, so the round trip back into the editor is lossless. Markup
// that already has its own line structure is left exactly as it arrived.
function prettyHTML(html: string): string {
    if (html.includes("\n")) return html.trim();
    return html
        .replace(/(<\/(?:p|div|h[1-6]|ul|ol|li|blockquote|table|tr|td|th|thead|tbody)>|<img\b[^>]*>)(?=<)/gi, "$1\n")
        .trim();
}

// The tags the visual editor's schema can hold. Anything else in HTML mode is
// dropped the moment the editor parses it, so the user is told which ones
// before that happens rather than after.
//
// This list has to match the extensions actually mounted above, or the warning
// stays silent while the switch destroys something. Headings are configured to
// levels 2 and 3, so h1 and h4-h6 become paragraphs. Nothing mounted parses
// font or center. The table extensions know only table, tr, td and th: thead
// and tfoot lose their section, colgroup and col are dropped, and a caption
// comes back as an extra row.
const SCHEMA_TAGS = new Set([
    "p", "br", "strong", "b", "em", "i", "u", "s", "strike", "del",
    "h2", "h3", "ul", "ol", "li", "a", "img", "span", "div",
    "table", "tbody", "tr", "td", "th",
]);

function unsupportedTags(html: string): string[] {
    const found = new Set<string>();
    for (const m of html.matchAll(/<\s*([a-zA-Z][a-zA-Z0-9]*)\b/g)) {
        const tag = m[1].toLowerCase();
        if (!SCHEMA_TAGS.has(tag)) found.add(tag);
    }
    return [...found].sort();
}

function Toolbar({
    editor,
    variables,
    links = [],
    sourceOpen,
    onToggleSource,
}: {
    editor: Editor;
    variables: string[];
    links?: string[];
    sourceOpen: boolean;
    // Absent where the step cannot persist a mode (the AI-block prompt), which
    // is also where a raw-markup body would mean nothing.
    onToggleSource?: () => void;
}) {
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

    // Writing controls are the source view's business, not the toolbar's: the
    // textarea holds markup, so a bold command there would be meaningless.
    if (sourceOpen && onToggleSource) {
        return (
            <div className="relative flex flex-wrap items-center gap-0.5 border-b border-slate-200/70 px-1.5 py-1">
                <span className="px-1.5 text-[10px] uppercase tracking-[0.14em] text-slate-400">HTML source</span>
                <div className="ml-auto">
                    <button
                        type="button"
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={onToggleSource}
                        title="Back to the visual editor"
                        className="h-7 px-2 inline-flex items-center gap-1.5 rounded text-[11.5px] font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
                    >
                        <PencilLineIcon className="w-3.5 h-3.5" />
                        Visual
                    </button>
                </div>
            </div>
        );
    }

    return (
        <div className="relative flex flex-wrap items-center gap-0.5 border-b border-slate-200/70 px-1.5 py-1">
            <Btn
                onClick={() => editor.chain().focus().undo().run()}
                disabled={!editor.can().undo()}
                title="Undo (Ctrl+Z)"
            >
                <Undo2Icon className="w-3.5 h-3.5" />
            </Btn>
            <Btn
                onClick={() => editor.chain().focus().redo().run()}
                disabled={!editor.can().redo()}
                title="Redo (Ctrl+Shift+Z)"
            >
                <Redo2Icon className="w-3.5 h-3.5" />
            </Btn>
            <Divider />
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
            <ImageMenu editor={editor} />
            <ButtonInsert editor={editor} />
            <Divider />
            <TypeMenu editor={editor} />
            <ColorMenu editor={editor} />
            <AlignMenu editor={editor} />
            <TableMenu editor={editor} />
            <Divider />
            <VariableMenu
                onPick={(v) => (links.includes(v) ? insertLinkToken(editor, v) : insertToken(editor, v))}
                variables={variables}
                links={links}
            />
            <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => editor.chain().focus().insertAIVariable().run()}
                title="Insert an AI block — writes unique copy for each recipient"
                className="h-7 px-1.5 inline-flex items-center gap-1 rounded text-sky-600 transition-colors hover:bg-sky-50 hover:text-sky-700"
            >
                <SparklesIcon className="w-3.5 h-3.5" />
                <span className="text-[11.5px] font-medium">AI</span>
            </button>
            <Btn
                onClick={() => editor.chain().focus().insertConditional().run()}
                title="Insert a condition — show text only when a field matches"
            >
                <GitBranchIcon className="w-3.5 h-3.5" />
            </Btn>
            <Btn
                onClick={() => editor.chain().focus().insertContent("{option one|option two}").run()}
                title="Insert spintax — randomly picks one option per send"
            >
                <ShuffleIcon className="w-3.5 h-3.5" />
            </Btn>
            <FormMenu onPick={(publicId) => editor.chain().focus().insertFormLink(publicId).run()} />

            {onToggleSource && (
                <div className="ml-auto">
                    <Btn onClick={onToggleSource} title="Edit the HTML source">
                        <CodeIcon className="w-3.5 h-3.5" />
                    </Btn>
                </div>
            )}

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
                        {links.includes(UNSUBSCRIBE_TOKEN) && (
                            <button
                                type="button"
                                onMouseDown={(e) => e.preventDefault()}
                                onClick={() => setLinkUrl(UNSUBSCRIBE_TOKEN)}
                                title="Point this link at the recipient's unsubscribe page"
                                className="h-7 px-2 inline-flex items-center rounded text-[11.5px] font-medium text-slate-500 hover:bg-slate-100 hover:text-slate-900"
                            >
                                Unsubscribe
                            </button>
                        )}
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
    disabled,
    children,
}: {
    active?: boolean;
    onClick: () => void;
    title: string;
    disabled?: boolean;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            title={title}
            aria-pressed={active}
            disabled={disabled}
            onMouseDown={(e) => e.preventDefault()}
            onClick={onClick}
            className={`size-7 inline-flex items-center justify-center rounded transition-colors disabled:opacity-40 disabled:hover:bg-transparent ${
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

// Shared variable inserter — a personalization menu that explains each field,
// suggests the org's real custom fields, and can insert a custom field by name.
// Flips horizontally so it never overflows the editor edge.
export function VariableMenu({
    onPick,
    variables,
    links = [],
}: {
    onPick: (token: string) => void;
    variables: string[];
    // Per-send link tokens (the recipient's unsubscribe link); body editors only.
    links?: string[];
}) {
    const [open, setOpen] = React.useState(false);
    const [custom, setCustom] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    // Ignores clicks inside the portaled [data-floating] panel, so only a click
    // truly outside the trigger+panel closes it.
    useClickOutside(ref, () => setOpen(false));
    // floating-ui keeps the panel glued to the trigger through scroll/resize.
    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 6,
        maxHeight: true,
    });

    const { data: customKeys = [] } = useCustomFieldKeys();
    const customName = cleanFieldName(custom);
    const insertCustom = () => {
        if (!customName) return;
        onPick(buildToken(customName));
        setCustom("");
        setOpen(false);
    };
    // Suggest the org's real custom-field keys, filtered by what's typed and
    // excluding any that shadow a standard field (the backend resolves those to
    // the standard value anyway).
    const q = customName.toLowerCase();
    const suggestions = customKeys
        .filter((k) => !isStandardKey(k) && (!q || k.toLowerCase().includes(q)))
        .slice(0, 8);
    const shadowsStandard = customName !== "" && isStandardKey(customName);

    return (
        <div ref={ref} className="relative">
            <button
                ref={(el) => setReference(el)}
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => setOpen((o) => !o)}
                title="Insert a personalization variable"
                className="h-7 px-1.5 inline-flex items-center gap-1 rounded text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
            >
                <BracesIcon className="w-3.5 h-3.5" />
                <ChevronDownIcon className="w-3 h-3" />
            </button>
            {typeof document !== "undefined" &&
                createPortal(
                    <AnimatePresence>
                        {open && (
                            <motion.div
                                ref={setFloating}
                                data-floating=""
                                style={floatingStyle}
                                initial={{ opacity: 0 }}
                                animate={{ opacity: 1 }}
                                exit={{ opacity: 0 }}
                                transition={{ duration: 0.12 }}
                                className="z-[60] w-[340px] max-w-[calc(100vw-24px)] overflow-y-auto rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]"
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

                        {links.length > 0 && (
                            <div className="px-2 pt-2">
                                <div className="px-1 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                    Links
                                </div>
                                <div className="grid grid-cols-2 gap-1">
                                    {links.map((v) => {
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
                        )}

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

                            {/* Real custom-field keys the org actually uses. */}
                            {suggestions.length > 0 && (
                                <div className="mt-1.5 flex flex-wrap gap-1">
                                    {suggestions.map((k) => (
                                        <button
                                            key={k}
                                            type="button"
                                            title={`Insert {{.${cleanFieldName(k)}}}`}
                                            onMouseDown={(e) => e.preventDefault()}
                                            onClick={() => {
                                                onPick(buildToken(k));
                                                setCustom("");
                                                setOpen(false);
                                            }}
                                            className="inline-flex max-w-full items-center truncate rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] text-slate-600 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                                        >
                                            {k}
                                        </button>
                                    ))}
                                </div>
                            )}

                            {shadowsStandard ? (
                                <p className="mt-1 text-[10px] text-amber-600">
                                    A contact field named <code className="font-mono">{customName}</code> is shadowed by
                                    the standard field above and always uses that value.
                                </p>
                            ) : (
                                <p className="mt-1 text-[10px] text-slate-400">
                                    Inserts{" "}
                                    <code className="font-mono text-slate-500">{`{{.${customName || "name"}}}`}</code> —
                                    exact field name; blank if the contact lacks it.
                                </p>
                            )}
                        </div>

                        <a
                            href="https://docs.warmbly.com/learn/personalization/"
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
                    </AnimatePresence>,
                    document.body,
                )}
        </div>
    );
}

// FormMenu inserts a personalized form link. Only published forms are offered:
// an unpublished form resolves to nothing at send time.
function FormMenu({ onPick }: { onPick: (publicId: string) => void }) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement | null>(null);
    useClickOutside(ref, () => setOpen(false));
    const { setReference, setFloating, floatingStyle } = useAnchoredFloating(open, {
        placement: "bottom-start",
        gap: 6,
        maxHeight: true,
    });
    const { data: forms = [] } = useForms(open);
    const published = forms.filter((f) => f.status === "published");

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.stopPropagation();
                setOpen(false);
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, [open]);

    return (
        <div ref={ref} className="relative">
            <button
                ref={(el) => setReference(el)}
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => setOpen((o) => !o)}
                title="Insert a form: every recipient gets their own link"
                className="h-7 px-1.5 inline-flex items-center gap-1 rounded text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
            >
                <ClipboardListIcon className="w-3.5 h-3.5" />
                <ChevronDownIcon className="w-3 h-3" />
            </button>
            {typeof document !== "undefined" &&
                createPortal(
                    <AnimatePresence>
                        {open && (
                            <motion.div
                                ref={setFloating}
                                data-floating=""
                                style={floatingStyle}
                                initial={{ opacity: 0 }}
                                animate={{ opacity: 1 }}
                                exit={{ opacity: 0 }}
                                transition={{ duration: 0.12 }}
                                className="z-[60] w-72 max-w-[calc(100vw-24px)] overflow-hidden rounded-md border border-slate-200 bg-white p-2 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]"
                            >
                                <div className="px-1 pb-1.5">
                                    <p className="text-[12px] font-medium text-slate-800">Insert a form</p>
                                    <p className="text-[10.5px] text-slate-400 mt-0.5">
                                        Each recipient gets their own link, so replies land back on their contact.
                                    </p>
                                </div>
                                {published.length === 0 ? (
                                    <div className="px-2 py-2 text-[12px] text-slate-500">
                                        No published forms yet. Publish one from the Forms page first.
                                    </div>
                                ) : (
                                    <div className="max-h-56 space-y-0.5 overflow-y-auto">
                                        {published.map((f) => (
                                            <button
                                                key={f.id}
                                                type="button"
                                                onMouseDown={(e) => e.preventDefault()}
                                                onClick={() => {
                                                    onPick(f.public_id);
                                                    setOpen(false);
                                                }}
                                                className="flex w-full items-center gap-1.5 rounded px-2 py-1.5 text-left text-[12px] text-slate-700 transition-colors hover:bg-slate-100"
                                            >
                                                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
                                                <span className="truncate">{f.name}</span>
                                            </button>
                                        ))}
                                    </div>
                                )}
                            </motion.div>
                        )}
                    </AnimatePresence>,
                    document.body,
                )}
        </div>
    );
}
