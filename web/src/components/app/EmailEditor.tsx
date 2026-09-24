// The mailbox signature editor: a visual surface, a raw HTML source view, a
// sandboxed preview and the plain-text alternative.
//
// A signature is real email markup (table layouts, inline styles, a logo)
// and it is appended to every campaign and every reply, so what it holds has
// to survive verbatim (issue #393). Two consequences shape this file:
//
//   - markup a contentEditable cannot host safely (a <style> block, a whole
//     document, an event handler) forces the source view. The visual surface
//     works by assigning innerHTML, and a signature is org data a teammate
//     wrote, so an <img onerror> there would run in someone else's dashboard.
//     Inline styles are allowed through, because almost every real signature
//     has them, so the surface is paint-contained instead: that makes it the
//     containing block for a positioned descendant, and a signature saying
//     position:fixed can no longer lay itself over a teammate's dashboard.
//   - the preview renders in the inbox's sandboxed frame, never in the
//     dashboard DOM, for the same reason.

import {
    RiBold,
    RiItalic,
    RiUnderline,
    RiLink,
    RiImage2Line,
    RiText,
    RiCodeView,
} from "@remixicon/react";
import DOMPurify, { type Config as DOMPurifyConfig } from "dompurify";
import { useEffect, useRef, useState } from "react";
import { RiEyeLine } from "@remixicon/react";
import { cn } from "@/lib/utils";
import EmailBody from "@/components/app/unibox/EmailBody";
import { detectPastedEmail } from "@/lib/email/pastedEmail";
import { TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { Checkbox } from "@/components/ui/checkbox";

// htmlToPlain renders the HTML signature down to a plain-text equivalent,
// turning block elements and <br> into line breaks. Used to keep the plain
// version in lockstep with the HTML one while "sync" is on.
function htmlToPlain(html: string): string {
    const withBreaks = html
        .replace(/<\s*br\s*\/?>/gi, "\n")
        .replace(/<\/\s*(p|div|h[1-6]|li|tr)\s*>/gi, "\n");
    if (typeof DOMParser === "undefined") return withBreaks.replace(/<[^>]+>/g, "");
    // DOMParser builds an inert document: nothing is fetched and no handler
    // runs. Assigning innerHTML on a detached div does fire <img onerror>.
    const doc = new DOMParser().parseFromString(withBreaks, "text/html");
    // Without this a signature carrying a <style> block put its whole
    // stylesheet into the plain-text alternative.
    doc.querySelectorAll("style, script, title, noscript, template").forEach((el) => el.remove());
    return (doc.body.textContent || "").replace(/[ \t]+/g, " ").replace(/\n{3,}/g, "\n\n").trim();
}

// Markup the visual surface cannot host. It edits by assigning innerHTML, so
// an event handler would run and a <style> block would restyle the dashboard;
// a whole document cannot survive the round trip at all. All of it is
// perfectly good in a signature, so the answer is the source view, not a
// refusal.
const UNHOSTABLE_TAG = /<\s*(script|style|iframe|object|embed|meta|link|html|head|body)\b|<!doctype/i;
// An event handler, matched only inside a tag: "on" followed by letters and an
// equals sign is also how a signature writes "reach me online = always".
const EVENT_HANDLER = /<[a-z][a-z0-9]*\b[^>]*\son[a-z]+\s*=/i;

function needsSource(html: string): boolean {
    return UNHOSTABLE_TAG.test(html) || EVENT_HANDLER.test(html);
}

// needsSource decides which editing surface to show. It must not be the only
// thing standing between a signature and script execution, because a regex
// does not tokenise HTML the way the parser does: `<img/onerror=alert(1) src=x>`
// separates the attribute with a slash rather than whitespace, and
// `<img src="x>" onerror=alert(1)>` hides the handler behind a `>` inside a
// quoted value. The parser accepts both; the pattern above matches neither.
//
// A signature is organisation data one teammate writes and another renders, so
// that is stored cross-user script in the dashboard. Everything assigned to a
// live element goes through the parser-based sanitizer instead.
const SIGNATURE_SANITIZE_CONFIG: DOMPurifyConfig = {
    FORBID_TAGS: ["script", "style", "iframe", "object", "embed", "form", "base", "meta", "link"],
    FORBID_ATTR: ["srcdoc", "formaction", "ping"],
    ALLOW_DATA_ATTR: false,
};

function sanitizeForEditing(html: string): string {
    // String(...) because the Trusted Types overload widens the return type;
    // RETURN_TRUSTED_TYPE is not set, so this is already a string at runtime.
    return String(DOMPurify.sanitize(html, SIGNATURE_SANITIZE_CONFIG));
}

interface EmailEditorProps {
    id: string;
    htmlText: string;
    setHtmlText: (v: string) => void;
    plainText: string;
    setPlainText: (v: string) => void;
    sync: boolean;
    setSync: (v: boolean) => void;
    code: boolean;
    setCode: (v: boolean) => void;
}

export default function EmailEditor({
    id,
    htmlText,
    setHtmlText,
    plainText,
    setPlainText,
    sync,
    setSync,
    code,
    setCode,
}: EmailEditorProps) {
    const editorRef = useRef<HTMLDivElement>(null);
    const [activeTab, setActiveTab] = useState<"html" | "preview" | "plain">("html");
    // A signature carrying a stylesheet, a whole document or an event handler
    // is edited as source whatever the toggle says: the visual surface hosts
    // it by assigning innerHTML, which would run it inside the dashboard.
    const forcedSource = needsSource(htmlText);
    const sourceView = code || forcedSource;
    // Only rewrite innerHTML when the prop diverges from the DOM; rewriting it
    // every render resets the caret. Never while the source view is showing:
    // that markup must not reach a live element at all.
    useEffect(() => {
        if (sourceView || activeTab !== "html") return;
        const el = editorRef.current;
        // Sanitized on the way in, not merely inspected: this is the assignment
        // that would execute a handler the source-view heuristic missed.
        const safe = sanitizeForEditing(htmlText);
        if (el && el.innerHTML !== safe) el.innerHTML = safe;
    }, [htmlText, activeTab, sourceView]);
    const [urlPopover, setUrlPopover] = useState<"link" | "image" | null>(null);
    const [url, setUrl] = useState("");
    // The contentEditable selection is lost as soon as the popover's text
    // input takes focus, so we snapshot the range on the toolbar button's
    // mousedown and restore it right before running the command.
    const savedRange = useRef<Range | null>(null);

    function saveSelection() {
        const sel = window.getSelection();
        savedRange.current =
            sel && sel.rangeCount > 0 ? sel.getRangeAt(0).cloneRange() : null;
    }

    function applyUrl() {
        const u = url.trim();
        const kind = urlPopover;
        setUrl("");
        setUrlPopover(null);
        if (!u || !kind) return;
        editorRef.current?.focus();
        const sel = window.getSelection();
        if (savedRange.current && sel) {
            sel.removeAllRanges();
            sel.addRange(savedRange.current);
        }
        exec(kind === "image" ? "insertImage" : "createLink", u);
    }

    // commitHtml writes the HTML signature and, while sync is on, keeps the
    // plain-text version derived from it so the two stay identical.
    function commitHtml(html: string) {
        setHtmlText(html);
        if (sync) setPlainText(htmlToPlain(html));
    }

    function exec(command: string, value?: string) {
        document.execCommand(command, false, value);
        if (editorRef.current) commitHtml(editorRef.current.innerHTML);
    }

    const toolbarButtons = [
        { icon: RiBold, command: "bold", title: "Bold" },
        { icon: RiItalic, command: "italic", title: "Italic" },
        { icon: RiUnderline, command: "underline", title: "Underline" },
    ];

    const tabBtn = (active: boolean) =>
        cn(
            "h-6 px-2 rounded text-[11px] font-medium transition-colors",
            active ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-800",
        );

    const iconBtn =
        "w-7 h-7 flex items-center justify-center rounded-md text-slate-500 hover:bg-slate-200/60 hover:text-slate-800 transition-colors";

    return (
        <div className="rounded-md border border-slate-200 overflow-hidden bg-white">
            <div className="flex flex-wrap items-center gap-0.5 px-2 py-1 min-h-9 md:flex-nowrap md:py-0 md:h-9 border-b border-slate-200 bg-slate-50/60">
                {/* HTML / Plain segmented toggle */}
                <div className="flex items-center gap-0.5 p-0.5 rounded-md bg-slate-100/70 mr-1">
                    <button type="button" onClick={() => setActiveTab("html")} className={tabBtn(activeTab === "html")}>
                        HTML
                    </button>
                    <button type="button" onClick={() => setActiveTab("preview")} className={tabBtn(activeTab === "preview")}>
                        Preview
                    </button>
                    <button type="button" onClick={() => setActiveTab("plain")} className={tabBtn(activeTab === "plain")}>
                        Plain
                    </button>
                </div>

                {activeTab === "html" && !sourceView && (
                    <>
                        {toolbarButtons.map((btn) => (
                            <button
                                key={btn.command}
                                type="button"
                                onMouseDown={(e) => {
                                    e.preventDefault();
                                    exec(btn.command);
                                }}
                                title={btn.title}
                                className={iconBtn}
                            >
                                <btn.icon className="w-3.5 h-3.5" />
                            </button>
                        ))}
                        <div className="w-px h-4 bg-slate-200 mx-1" />
                        <PopoverMenu
                            open={urlPopover === "link"}
                            onOpenChange={(o) => {
                                setUrl("");
                                setUrlPopover(o ? "link" : null);
                            }}
                        >
                            <PopoverMenuTrigger asChild>
                                <button
                                    type="button"
                                    onMouseDown={(e) => {
                                        e.preventDefault();
                                        saveSelection();
                                    }}
                                    title="Insert link"
                                    className={iconBtn}
                                >
                                    <RiLink className="w-3.5 h-3.5" />
                                </button>
                            </PopoverMenuTrigger>
                            <PopoverMenuContent minWidth={240} className="p-2">
                                <UrlForm
                                    placeholder="https://example.com"
                                    url={url}
                                    setUrl={setUrl}
                                    onApply={applyUrl}
                                    onCancel={() => setUrlPopover(null)}
                                />
                            </PopoverMenuContent>
                        </PopoverMenu>
                        <PopoverMenu
                            open={urlPopover === "image"}
                            onOpenChange={(o) => {
                                setUrl("");
                                setUrlPopover(o ? "image" : null);
                            }}
                        >
                            <PopoverMenuTrigger asChild>
                                <button
                                    type="button"
                                    onMouseDown={(e) => {
                                        e.preventDefault();
                                        saveSelection();
                                    }}
                                    title="Insert image"
                                    className={iconBtn}
                                >
                                    <RiImage2Line className="w-3.5 h-3.5" />
                                </button>
                            </PopoverMenuTrigger>
                            <PopoverMenuContent minWidth={240} className="p-2">
                                <UrlForm
                                    placeholder="https://…/image.png"
                                    url={url}
                                    setUrl={setUrl}
                                    onApply={applyUrl}
                                    onCancel={() => setUrlPopover(null)}
                                />
                            </PopoverMenuContent>
                        </PopoverMenu>
                    </>
                )}

                <div className="ml-auto flex items-center gap-1.5">
                    {activeTab === "html" && (
                        <button
                            type="button"
                            onClick={() => setCode(!sourceView)}
                            disabled={forcedSource}
                            title={
                                forcedSource
                                    ? "This signature holds markup the visual editor cannot host safely"
                                    : sourceView
                                      ? "Visual editor"
                                      : "Edit HTML source"
                            }
                            className={cn(
                                iconBtn,
                                sourceView && "bg-slate-200/70 text-slate-800",
                                forcedSource && "opacity-40 cursor-not-allowed hover:bg-transparent",
                            )}
                        >
                            {sourceView ? <RiText className="w-3.5 h-3.5" /> : <RiCodeView className="w-3.5 h-3.5" />}
                        </button>
                    )}
                    <label
                        className="flex items-center gap-1.5 text-[11px] text-slate-500 cursor-pointer select-none pl-1"
                        title="Generate the plain-text version from the HTML and keep them identical"
                    >
                        <Checkbox size="xs"
                            checked={sync}
                            onChange={(e) => {
                                const on = e.target.checked;
                                setSync(on);
                                if (on) setPlainText(htmlToPlain(htmlText));
                            }}
                        />
                        <span className="hidden sm:inline">Sync HTML &amp; plain</span>
                        <span className="sm:hidden">Sync</span>
                    </label>
                </div>
            </div>

            {activeTab === "plain" ? (
                <div>
                    {sync && (
                        <div className="px-3 pt-2 text-[10.5px] text-slate-400">
                            Generated from the HTML version. Turn off sync to edit it separately.
                        </div>
                    )}
                    <textarea
                        value={plainText}
                        onChange={(e) => setPlainText(e.target.value)}
                        readOnly={sync}
                        className={cn(
                            "w-full min-h-[120px] px-3 py-2.5 text-[13px] text-slate-800 outline-none resize-none font-mono",
                            sync && "bg-slate-50/60 text-slate-500 cursor-not-allowed",
                        )}
                        placeholder="Plain text version…"
                    />
                </div>
            ) : activeTab === "preview" ? (
                // The inbox's sandboxed frame, so a signature written as a
                // whole document previews as a mail client renders it and can
                // neither run nor restyle the dashboard for a teammate.
                <div className="min-h-[120px] px-3 py-2.5">
                    {htmlText.trim() || plainText.trim() ? (
                        <EmailBody html={htmlText} plain={plainText} />
                    ) : (
                        <p className="text-[12px] italic text-slate-400">Nothing to preview yet.</p>
                    )}
                </div>
            ) : sourceView ? (
                <div>
                    <textarea
                        value={htmlText}
                        onChange={(e) => commitHtml(e.target.value)}
                        className="w-full min-h-[120px] px-3 py-2.5 text-[12px] text-slate-800 outline-none resize-y font-mono"
                        placeholder="<p>HTML source…</p>"
                        spellCheck={false}
                    />
                    <p className="flex items-start gap-1.5 border-t border-slate-200/70 px-3 py-1.5 text-[10.5px] leading-relaxed text-slate-400">
                        <RiEyeLine className="mt-px w-3 h-3 shrink-0" />
                        <span>
                            {forcedSource
                                ? "Edited as source because this signature carries a stylesheet, a document wrapper or an event handler. It is sent exactly as written; use Preview to see it."
                                : "Sent exactly as written. Any <style> block is copied onto the elements it matches at send time, so it survives Outlook and Yahoo."}
                        </span>
                    </p>
                </div>
            ) : (
                <div
                    ref={editorRef}
                    id={id}
                    contentEditable
                    suppressContentEditableWarning
                    onInput={(e) => commitHtml(e.currentTarget.innerHTML)}
                    onPaste={(e) => {
                        // A signature copied as markup is the tags, not the
                        // text of the tags. Adopting it switches to source,
                        // which is the only view that can hold it.
                        const adopted = detectPastedEmail(e.clipboardData);
                        if (!adopted) return;
                        e.preventDefault();
                        commitHtml(adopted);
                        setCode(true);
                    }}
                    className="email-signature-surface min-h-[120px] px-3 py-2.5 text-[13px] text-slate-800 outline-none prose prose-sm max-w-none"
                />
            )}
        </div>
    );
}

// UrlForm — the small themed popover body used by the insert-link and
// insert-image toolbar buttons (replaces the native prompt()).
function UrlForm({
    placeholder,
    url,
    setUrl,
    onApply,
    onCancel,
}: {
    placeholder: string;
    url: string;
    setUrl: (v: string) => void;
    onApply: () => void;
    onCancel: () => void;
}) {
    return (
        <div className="flex items-center gap-1.5">
            <TextInput
                value={url}
                onChange={setUrl}
                placeholder={placeholder}
                autoFocus
                className="flex-1"
                onKeyDown={(e) => {
                    if (e.key === "Enter") {
                        e.preventDefault();
                        onApply();
                    }
                    if (e.key === "Escape") onCancel();
                }}
            />
            <button
                type="button"
                onClick={onApply}
                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors shrink-0"
            >
                Apply
            </button>
        </div>
    );
}
