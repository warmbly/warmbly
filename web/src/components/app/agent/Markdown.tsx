// Dependency-free markdown renderer for assistant messages. Handles the small
// subset the agent is prompted to emit (paragraphs, lists, bold/italic/code,
// links, fenced code) as real React nodes — no raw HTML, so nothing the model
// outputs can inject markup. Parsing is tolerant of the partial text that
// arrives mid-stream: an unclosed fence or token just renders literally until
// its closer streams in.

import React from "react";

const INLINE =
    /(`[^`\n]+`)|(\*\*[^*\n]+\*\*)|(\*[^*\n]+\*)|\[([^\]\n]+)\]\(([^)\s]+)\)/g;

function inline(
    text: string,
    onOpen?: (url: string) => void,
): React.ReactNode[] {
    const out: React.ReactNode[] = [];
    let last = 0;
    let k = 0;
    INLINE.lastIndex = 0;
    for (let m = INLINE.exec(text); m; m = INLINE.exec(text)) {
        if (m.index > last) out.push(text.slice(last, m.index));
        if (m[1]) {
            out.push(
                <code
                    key={k++}
                    className="rounded border border-slate-200 bg-slate-100 px-1 py-px font-mono text-[11.5px] text-slate-700"
                >
                    {m[1].slice(1, -1)}
                </code>,
            );
        } else if (m[2]) {
            out.push(
                <strong key={k++} className="font-semibold text-slate-900">
                    {m[2].slice(2, -2)}
                </strong>,
            );
        } else if (m[3]) {
            out.push(<em key={k++}>{m[3].slice(1, -1)}</em>);
        } else {
            const label = m[4];
            const url = m[5];
            const internal = url.startsWith("/");
            if (!internal && !/^https?:\/\//.test(url)) {
                out.push(label);
            } else {
                out.push(
                    <a
                        key={k++}
                        href={url}
                        target={internal ? undefined : "_blank"}
                        rel={internal ? undefined : "noreferrer"}
                        onClick={(e) => {
                            if (internal && onOpen) {
                                e.preventDefault();
                                onOpen(url);
                            }
                        }}
                        className="text-sky-700 underline decoration-sky-300 underline-offset-2 hover:decoration-sky-500"
                    >
                        {label}
                    </a>,
                );
            }
        }
        last = m.index + m[0].length;
    }
    if (last < text.length) out.push(text.slice(last));
    return out;
}

export default function Markdown({
    text,
    onOpen,
}: {
    text: string;
    onOpen?: (url: string) => void;
}) {
    const blocks: React.ReactNode[] = [];
    // Fence splitting leaves prose at even indexes, code at odd ones; a
    // still-streaming unclosed fence ends up as a trailing code segment.
    const parts = text.split(/```[^\n]*\n?/);
    parts.forEach((part, pi) => {
        if (pi % 2 === 1) {
            if (!part.trim()) return;
            blocks.push(
                <pre
                    key={`c${pi}`}
                    className="overflow-x-auto whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50 px-2.5 py-2 font-mono text-[11.5px] leading-relaxed text-slate-700"
                >
                    {part.replace(/\n$/, "")}
                </pre>,
            );
            return;
        }
        let para: string[] = [];
        let list: { ordered: boolean; items: string[] } | null = null;
        const flushPara = () => {
            if (!para.length) return;
            blocks.push(
                <p key={blocks.length} className="whitespace-pre-wrap">
                    {inline(para.join("\n"), onOpen)}
                </p>,
            );
            para = [];
        };
        const flushList = () => {
            if (!list) return;
            const L = list.ordered ? "ol" : "ul";
            blocks.push(
                <L
                    key={blocks.length}
                    className={
                        (list.ordered ? "list-decimal" : "list-disc") +
                        " space-y-1 pl-4 marker:text-slate-400"
                    }
                >
                    {list.items.map((it, i) => (
                        <li key={i}>{inline(it, onOpen)}</li>
                    ))}
                </L>,
            );
            list = null;
        };
        for (const line of part.split("\n")) {
            const ul = /^\s*[-*]\s+(.*)$/.exec(line);
            const ol = /^\s*\d+[.)]\s+(.*)$/.exec(line);
            const h = /^\s*#{1,4}\s+(.*)$/.exec(line);
            if (ul || ol) {
                flushPara();
                const ordered = !!ol;
                if (!list || list.ordered !== ordered) {
                    flushList();
                    list = { ordered, items: [] };
                }
                list.items.push((ol ?? ul)![1]);
            } else if (h) {
                flushPara();
                flushList();
                blocks.push(
                    <p key={blocks.length} className="font-semibold text-slate-900">
                        {inline(h[1], onOpen)}
                    </p>,
                );
            } else if (!line.trim()) {
                flushPara();
                flushList();
            } else {
                flushList();
                para.push(line);
            }
        }
        flushPara();
        flushList();
    });
    return (
        <div className="space-y-2 break-words text-[13px] leading-relaxed text-slate-800">
            {blocks}
        </div>
    );
}
