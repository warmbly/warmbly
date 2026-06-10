import {
    RiBold,
    RiItalic,
    RiUnderline,
    RiLink,
    RiImage2Line,
    RiText,
    RiCodeView,
} from "@remixicon/react";
import { useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";

// htmlToPlain renders the HTML signature down to a plain-text equivalent,
// turning block elements and <br> into line breaks. Used to keep the plain
// version in lockstep with the HTML one while "sync" is on.
function htmlToPlain(html: string): string {
    const withBreaks = html
        .replace(/<\s*br\s*\/?>/gi, "\n")
        .replace(/<\/\s*(p|div|h[1-6]|li|tr)\s*>/gi, "\n");
    const tmp = document.createElement("div");
    tmp.innerHTML = withBreaks;
    return (tmp.textContent || "").replace(/\n{3,}/g, "\n\n").trim();
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
    const [activeTab, setActiveTab] = useState<"html" | "plain">("html");
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
                    <button type="button" onClick={() => setActiveTab("plain")} className={tabBtn(activeTab === "plain")}>
                        Plain
                    </button>
                </div>

                {activeTab === "html" && !code && (
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
                            onClick={() => setCode(!code)}
                            title={code ? "Visual editor" : "Edit HTML source"}
                            className={cn(iconBtn, code && "bg-slate-200/70 text-slate-800")}
                        >
                            {code ? <RiText className="w-3.5 h-3.5" /> : <RiCodeView className="w-3.5 h-3.5" />}
                        </button>
                    )}
                    <label
                        className="flex items-center gap-1.5 text-[11px] text-slate-500 cursor-pointer select-none pl-1"
                        title="Generate the plain-text version from the HTML and keep them identical"
                    >
                        <input
                            type="checkbox"
                            checked={sync}
                            onChange={(e) => {
                                const on = e.target.checked;
                                setSync(on);
                                if (on) setPlainText(htmlToPlain(htmlText));
                            }}
                            className="w-3 h-3 rounded accent-sky-600"
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
            ) : code ? (
                <textarea
                    value={htmlText}
                    onChange={(e) => commitHtml(e.target.value)}
                    className="w-full min-h-[120px] px-3 py-2.5 text-[12px] text-slate-800 outline-none resize-none font-mono"
                    placeholder="<p>HTML source…</p>"
                    spellCheck={false}
                />
            ) : (
                <div
                    ref={editorRef}
                    id={id}
                    contentEditable
                    onInput={(e) => commitHtml(e.currentTarget.innerHTML)}
                    className="min-h-[120px] px-3 py-2.5 text-[13px] text-slate-800 outline-none prose prose-sm max-w-none"
                    dangerouslySetInnerHTML={{ __html: htmlText }}
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
