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

    function exec(command: string, value?: string) {
        document.execCommand(command, false, value);
        if (editorRef.current) setHtmlText(editorRef.current.innerHTML);
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
            <div className="flex items-center gap-0.5 px-2 h-9 border-b border-slate-200 bg-slate-50/60">
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
                        <button
                            type="button"
                            onMouseDown={(e) => {
                                e.preventDefault();
                                const url = prompt("Enter URL:");
                                if (url) exec("createLink", url);
                            }}
                            title="Insert link"
                            className={iconBtn}
                        >
                            <RiLink className="w-3.5 h-3.5" />
                        </button>
                        <button
                            type="button"
                            onMouseDown={(e) => {
                                e.preventDefault();
                                const url = prompt("Enter image URL:");
                                if (url) exec("insertImage", url);
                            }}
                            title="Insert image"
                            className={iconBtn}
                        >
                            <RiImage2Line className="w-3.5 h-3.5" />
                        </button>
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
                    <label className="flex items-center gap-1.5 text-[11px] text-slate-500 cursor-pointer select-none pl-1">
                        <input
                            type="checkbox"
                            checked={sync}
                            onChange={(e) => setSync(e.target.checked)}
                            className="w-3 h-3 rounded accent-sky-600"
                        />
                        Sync to replies
                    </label>
                </div>
            </div>

            {activeTab === "plain" ? (
                <textarea
                    value={plainText}
                    onChange={(e) => setPlainText(e.target.value)}
                    className="w-full min-h-[120px] px-3 py-2.5 text-[13px] text-slate-800 outline-none resize-none font-mono"
                    placeholder="Plain text version…"
                />
            ) : code ? (
                <textarea
                    value={htmlText}
                    onChange={(e) => setHtmlText(e.target.value)}
                    className="w-full min-h-[120px] px-3 py-2.5 text-[12px] text-slate-800 outline-none resize-none font-mono"
                    placeholder="<p>HTML source…</p>"
                    spellCheck={false}
                />
            ) : (
                <div
                    ref={editorRef}
                    id={id}
                    contentEditable
                    onInput={(e) => setHtmlText(e.currentTarget.innerHTML)}
                    className="min-h-[120px] px-3 py-2.5 text-[13px] text-slate-800 outline-none prose prose-sm max-w-none"
                    dangerouslySetInnerHTML={{ __html: htmlText }}
                />
            )}
        </div>
    );
}
