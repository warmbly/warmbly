// Renders a received message body.
//
// Email HTML is other people's markup: full documents with their own <style>
// blocks, table layouts, and font stacks. Dropping that into the dashboard DOM
// would let a newsletter restyle the app, so it renders inside a sandboxed
// iframe instead. The iframe carries no `allow-scripts`, so nothing in the
// message can execute even though the API already sanitizes the HTML on the
// way out; the two together are belt and braces.
//
// Height is measured from the inner document and kept in sync as images load,
// so the message reads as part of the page rather than a scroll box.

import React from "react";
import { plainToDisplayHtml } from "@/lib/email/body";

interface EmailBodyProps {
    html?: string | null;
    plain?: string | null;
}

// Typography for the message document. Deliberately minimal: the message
// brings its own styling, and this only sets what it does not.
const DOCUMENT_CSS = `
  html, body { margin: 0; padding: 0; }
  body {
    font-family: Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
    font-size: 13px;
    line-height: 1.6;
    color: #1e293b;
    background: transparent;
    word-break: break-word;
    overflow-wrap: anywhere;
  }
  img { max-width: 100%; height: auto; border: 0; }
  table { max-width: 100%; }
  a { color: #0284c7; }
  blockquote {
    margin: 0.5em 0;
    padding-left: 0.75em;
    border-left: 2px solid #e2e8f0;
    color: #475569;
  }
  pre { white-space: pre-wrap; }
`;

function buildDocument(body: string): string {
    return `<!doctype html><html><head><meta charset="utf-8">` +
        `<meta name="referrer" content="no-referrer">` +
        // Every link in the message leaves the dashboard in a new tab.
        `<base target="_blank">` +
        `<style>${DOCUMENT_CSS}</style></head><body>${body}</body></html>`;
}

export default function EmailBody({ html, plain }: EmailBodyProps) {
    const frameRef = React.useRef<HTMLIFrameElement>(null);
    const [height, setHeight] = React.useState(0);

    const srcDoc = React.useMemo(() => {
        const trimmedHtml = (html ?? "").trim();
        if (trimmedHtml) return buildDocument(trimmedHtml);
        const trimmedPlain = (plain ?? "").trim();
        if (trimmedPlain) return buildDocument(plainToDisplayHtml(trimmedPlain));
        return "";
    }, [html, plain]);

    // Late-loading remote images change the document height after onLoad, so
    // measurement repeats until the size settles rather than running once.
    const measure = React.useCallback(() => {
        const doc = frameRef.current?.contentDocument;
        if (!doc?.body) return;
        const next = Math.ceil(
            Math.max(doc.body.scrollHeight, doc.documentElement?.scrollHeight ?? 0),
        );
        setHeight((prev) => (Math.abs(prev - next) > 1 ? next : prev));
    }, []);

    const observerRef = React.useRef<ResizeObserver | null>(null);
    React.useEffect(() => () => observerRef.current?.disconnect(), []);

    const onLoad = React.useCallback(() => {
        measure();
        const doc = frameRef.current?.contentDocument;
        if (!doc?.body) return;
        // A reload replaces the document the previous observer watched.
        observerRef.current?.disconnect();
        const observer = new ResizeObserver(measure);
        observer.observe(doc.body);
        observerRef.current = observer;
        doc.querySelectorAll("img").forEach((img) => {
            img.addEventListener("load", measure);
            img.addEventListener("error", measure);
        });
    }, [measure]);

    if (!srcDoc) {
        return (
            <p className="text-[13px] text-slate-400 italic">This message has no content.</p>
        );
    }

    return (
        <iframe
            ref={frameRef}
            title="Message body"
            srcDoc={srcDoc}
            onLoad={onLoad}
            // No allow-scripts: message markup can never run code. allow-popups
            // (plus escape-to-normal-context) is what lets a link actually open.
            sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
            referrerPolicy="no-referrer"
            className="w-full border-0 block"
            style={{ height: height ? `${height}px` : "80px" }}
        />
    );
}
