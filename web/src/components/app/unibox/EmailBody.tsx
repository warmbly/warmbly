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
//
// Every frame carries a Content-Security-Policy that refuses scripts, plugins,
// <base> and form targets, and a link that is not http(s), mailto or tel loses
// its href. With blockRemote the policy also refuses every remote image,
// background and font until the reader asks for them, so a sender's tracking
// pixel cannot learn when, where or in what client a message was read.

import React from "react";
import { ImageOffIcon, MoreHorizontalIcon } from "lucide-react";
import { hasRemoteContent, plainToDisplayHtml } from "@/lib/email/body";
import { isSafeLinkHref } from "@/lib/safeUrl";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";

interface EmailBodyProps {
    html?: string | null;
    plain?: string | null;
    // Hold back remote content until the reader loads it. Set for mail other
    // people wrote; previews of the user's own drafts leave it off.
    blockRemote?: boolean;
}

const CSP_BASE = "default-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";
// Inline (data:) images and fonts only: nothing leaves the browser.
const CSP_BLOCKED = `${CSP_BASE}; img-src data:; font-src data:; style-src 'unsafe-inline'`;
// After "Load images", and for previews of the user's own drafts: remote
// images, fonts and media, still no scripts or frames.
const CSP_LOADED = `${CSP_BASE}; img-src data: https: http:; font-src data: https:; media-src https:; style-src 'unsafe-inline'`;

// Collapse only recognizable history; ambiguous inline replies stay visible.
const QUOTE_SELECTORS = [
    ".gmail_quote",
    ".gmail_quote_container",
    'blockquote[type="cite"]',
    ".yahoo_quoted",
    ".moz-cite-prefix",
    "#divRplyFwdMsg",
    "#appendonsend",
    "[data-warmbly-quote]",
].join(", ");

const PLAIN_ATTRIBUTION = /^(?:On\b.{0,200}\bwrote:|-{2,}\s*(?:Original Message|Forwarded message)\s*-{2,})$/;

function plainToBody(text: string): string {
    const lines = text.replace(/\r\n/g, "\n").split("\n");
    const at = lines.findIndex((line) => PLAIN_ATTRIBUTION.test(line.trim()) || line.trim().startsWith(">"));
    if (at <= 0 || !lines.slice(0, at).join("").trim()) return plainToDisplayHtml(text);
    // An unprefixed answer after a > quote may be an inline reply, not history.
    const quotedAt = lines.findIndex((line, index) => index >= at && line.trim().startsWith(">"));
    if (quotedAt >= 0 && lines.slice(quotedAt).some((line) => line.trim() && !line.trim().startsWith(">"))) {
        return plainToDisplayHtml(text);
    }
    return `${plainToDisplayHtml(lines.slice(0, at).join("\n"))}<div data-warmbly-quote>${plainToDisplayHtml(lines.slice(at).join("\n"))}</div>`;
}

function prepareQuotes(body: string): { expanded: string; collapsed: string; hasQuote: boolean } {
    const doc = new DOMParser().parseFromString(body, "text/html");
    const quotes = new Set<HTMLElement>(doc.body.querySelectorAll(QUOTE_SELECTORS));
    doc.body.querySelectorAll(".moz-cite-prefix").forEach((marker) => {
        const quote = marker.nextElementSibling;
        if (quote instanceof HTMLElement && quote.tagName === "BLOCKQUOTE") quotes.add(quote);
    });
    // Outlook puts history after its header rather than inside it.
    doc.body.querySelectorAll<HTMLElement>("#divRplyFwdMsg, #appendonsend").forEach((marker) => {
        const tail = doc.createElement("div");
        marker.before(tail);
        while (tail.nextSibling) tail.append(tail.nextSibling);
        quotes.add(tail);
    });
    // Test Outlook tails as a unit, including any text-only siblings.
    quotes.forEach((node) => node.setAttribute("data-warmbly-quote", ""));
    const unquoted = doc.body.cloneNode(true) as HTMLElement;
    unquoted.querySelectorAll("[data-warmbly-quote], script, style").forEach((node) => node.remove());
    const hasContent = !!unquoted.textContent?.trim() || !!unquoted.querySelector("img, hr");
    if (!quotes.size || !hasContent) return { expanded: body, collapsed: body, hasQuote: false };
    const expanded = doc.documentElement.outerHTML;
    quotes.forEach((node) => node.style.setProperty("display", "none", "important"));
    return { expanded, collapsed: doc.documentElement.outerHTML, hasQuote: true };
}

// Default typography goes before the sender's styles; containment remains enforced.
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
  /* Containment, not styling: a 600px design must not scroll the drawer
     sideways, so these hold even against the message's own stylesheet. */
  img { max-width: 100% !important; height: auto; border: 0; }
  table, div { box-sizing: border-box !important; max-width: 100% !important; min-width: 0 !important; }
  a { color: #0284c7; }
  blockquote {
    margin: 0.5em 0;
    padding-left: 0.75em;
    border-left: 2px solid #e2e8f0;
    color: #475569;
  }
  pre { white-space: pre-wrap; }
`;

// The dark theme reads unstyled mail in the app's own colours. Anything that
// sets a colour or background of its own was designed on white, so it keeps
// its light document and sits on a white sheet instead.
const DARK_DOCUMENT_CSS = `
  :root { color-scheme: dark; }
  body { color: #d0d6e0; }
  a { color: #7dd3fc; }
  blockquote { border-left-color: #2e2f34; color: #8a8f98; }
`;

const DESIGNED = /<style[\s>]|<[^>]*\s(?:bgcolor|color|text)\s*=|\b(?:background|color)\s*:/i;

const isDesignedEmail = (body: string) => DESIGNED.test(body);

// A body that is already a whole document (a campaign written in HTML mode, a
// designed newsletter) must not be nested inside another one: the doctype and
// the <head> would land in the body, and the frame would preview something the
// recipient will never see. Its own <head> gets our shell instead.
const DOCUMENT_ROOT = /^\s*(?:<!--[\s\S]*?-->\s*)*(?:<!doctype\s+html|<html[\s>])/i;

function shell(csp: string, dark: boolean): string {
    // The policy must precede everything else in the head to govern it.
    return (
        `<meta http-equiv="Content-Security-Policy" content="${csp}">` +
        `<meta charset="utf-8"><meta name="referrer" content="no-referrer">` +
        `<base target="_blank"><style>${DOCUMENT_CSS}${dark ? DARK_DOCUMENT_CSS : ""}</style>`
    );
}

const LINK_ATTRS = ["href", "xlink:href", "action", "formaction"];

// Drops every link target whose scheme a link may not open (javascript:, data:, ...).
function neutralizeLinks(doc: Document) {
    doc.querySelectorAll("*").forEach((el) => {
        for (const name of LINK_ATTRS) {
            const value = el.getAttribute(name);
            if (value !== null && !isSafeLinkHref(value)) el.removeAttribute(name);
        }
    });
}

function buildDocument(body: string, csp: string, dark: boolean): string {
    // Our shell goes FIRST in the parsed document's real head, so the
    // message's own stylesheet comes after it and wins on everything but the
    // containment rules, and nothing in the markup (a commented-out <head>,
    // say) can capture where the policy lands.
    const whole = DOCUMENT_ROOT.test(body);
    const doc = new DOMParser().parseFromString(
        whole ? body : `<!doctype html><html><head></head><body>${body}</body></html>`,
        "text/html",
    );
    doc.head.insertAdjacentHTML("afterbegin", shell(csp, dark));
    neutralizeLinks(doc);
    return `${doc.doctype ? "<!doctype html>" : ""}${doc.documentElement.outerHTML}`;
}

export default function EmailBody({ html, plain, blockRemote = false }: EmailBodyProps) {
    const frameRef = React.useRef<HTMLIFrameElement>(null);
    const viewportRef = React.useRef<HTMLDivElement>(null);
    const [showQuoted, setShowQuoted] = React.useState(false);

    // The message body, before the shell. Split from srcDoc so toggling the
    // quote does not re-run the plain-text conversion.
    const body = React.useMemo(() => {
        const trimmedHtml = (html ?? "").trim();
        if (trimmedHtml) return trimmedHtml;
        const trimmedPlain = (plain ?? "").trim();
        if (trimmedPlain) return plainToBody(trimmedPlain);
        return "";
    }, [html, plain]);

    // Consent belongs to the body it was given for, so a different message is
    // blocked from its very first render.
    const [loadedBody, setLoadedBody] = React.useState<string | null>(null);
    const remoteLoaded = loadedBody !== null && loadedBody === body;

    const quotes = React.useMemo(() => prepareQuotes(body), [body]);
    const hasQuote = quotes.hasQuote;
    const remote = React.useMemo(() => blockRemote && hasRemoteContent(body), [blockRemote, body]);
    const csp = blockRemote && !remoteLoaded ? CSP_BLOCKED : CSP_LOADED;
    const darkTheme = useAppStore((s) => s.resolvedTheme === "dark");
    const designed = React.useMemo(() => isDesignedEmail(body), [body]);
    const darkDocument = darkTheme && !designed;
    const srcDoc = React.useMemo(
        () => (body ? buildDocument(showQuoted ? quotes.expanded : quotes.collapsed, csp, darkDocument) : ""),
        [body, quotes, showQuoted, csp, darkDocument],
    );

    // Measure at the available width first; scale only layouts that cannot reflow.
    const measure = React.useCallback(() => {
        const frame = frameRef.current;
        const viewport = viewportRef.current;
        const doc = frame?.contentDocument;
        const available = viewport?.clientWidth ?? 0;
        if (!frame || !viewport || !doc?.body || !available) return;
        frame.style.width = `${available}px`;
        // Document scrollHeight includes the viewport, so remove its previous floor.
        frame.style.height = "0px";
        const width = Math.max(available, doc.body.scrollWidth, doc.documentElement.scrollWidth);
        if (width > available) frame.style.width = `${width}px`;
        const height = Math.ceil(Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight));
        const scale = available / width;
        frame.style.height = `${height}px`;
        frame.style.transform = scale < 1 ? `scale(${scale})` : "";
        viewport.style.height = `${Math.ceil(height * scale)}px`;
    }, []);

    const observerRef = React.useRef<ResizeObserver | null>(null);
    const animationRef = React.useRef<number | null>(null);
    const scheduleMeasure = React.useCallback(() => {
        if (animationRef.current !== null) cancelAnimationFrame(animationRef.current);
        animationRef.current = requestAnimationFrame(() => {
            animationRef.current = null;
            measure();
        });
    }, [measure]);
    React.useEffect(() => () => {
        observerRef.current?.disconnect();
        if (animationRef.current !== null) cancelAnimationFrame(animationRef.current);
    }, []);

    const onLoad = React.useCallback(() => {
        const doc = frameRef.current?.contentDocument;
        if (!doc?.documentElement) return;
        // A network document has its own CSP, unlike srcdoc which inherits the dashboard's.
        const parsed = new DOMParser().parseFromString(srcDoc, "text/html");
        doc.documentElement.replaceChildren(doc.importNode(parsed.head, true), doc.importNode(parsed.body, true));
        // Sender styles must not turn the document roots into another scroll area.
        for (const root of [doc.documentElement, doc.body]) {
            for (const [name, value] of Object.entries({
                margin: "0", padding: "0", width: "auto", "min-width": "0",
                height: "auto", "min-height": "0", "max-height": "none",
            })) root.style.setProperty(name, value, "important");
        }
        doc.documentElement.style.setProperty("overflow", "hidden", "important");
        doc.body.style.setProperty("overflow", "visible", "important");
        doc.body.style.setProperty("display", "flow-root", "important");
        measure();
        // A reload replaces the document the previous observer watched.
        observerRef.current?.disconnect();
        let viewportWidth = viewportRef.current?.clientWidth;
        const observer = new ResizeObserver((entries) => {
            const width = viewportRef.current?.clientWidth;
            if (width !== viewportWidth || entries.some((entry) => entry.target === doc.body)) {
                viewportWidth = width;
                scheduleMeasure();
            }
        });
        observer.observe(doc.body);
        if (viewportRef.current) observer.observe(viewportRef.current);
        observerRef.current = observer;
        doc.querySelectorAll("img").forEach((img) => {
            img.addEventListener("load", scheduleMeasure);
            img.addEventListener("error", scheduleMeasure);
        });
        void doc.fonts?.ready.then(() => {
            if (frameRef.current?.contentDocument === doc) scheduleMeasure();
        });
    }, [measure, scheduleMeasure, srcDoc]);

    if (!srcDoc) {
        return (
            <p className="text-[13px] text-slate-400 italic">This message has no content.</p>
        );
    }

    return (
        <>
            {remote && !remoteLoaded && (
                <div className="mb-2 flex items-center gap-2 rounded-md border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-[11.5px] text-slate-500">
                    <ImageOffIcon className="w-3.5 h-3.5 shrink-0 text-slate-400" />
                    <span className="flex-1 min-w-0">
                        Remote images are hidden, so the sender can't see when you read this.
                    </span>
                    <button
                        type="button"
                        onClick={() => setLoadedBody(body)}
                        className="shrink-0 font-medium text-sky-700 hover:text-sky-800"
                    >
                        Load images
                    </button>
                </div>
            )}
            <div className={cn("min-w-0", darkTheme && designed && "theme-light rounded-md bg-white p-3")}>
                <div ref={viewportRef} className="relative w-full overflow-hidden" style={{ height: "80px" }}>
                    <iframe
                        key={srcDoc}
                        ref={frameRef}
                        title="Message body"
                        src="/mail-preview.html"
                        onLoad={onLoad}
                        // No allow-scripts: message markup can never run code.
                        sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
                        referrerPolicy="no-referrer"
                        scrolling="no"
                        className="w-full border-0 block origin-top-left"
                        style={{ height: "80px" }}
                    />
                </div>
            </div>
            {hasQuote && (
                <button
                    type="button"
                    onClick={() => setShowQuoted((v) => !v)}
                    aria-expanded={showQuoted}
                    title={showQuoted ? "Hide the quoted conversation" : "Show the quoted conversation"}
                    className="mt-1 h-5 px-1.5 rounded bg-slate-100 hover:bg-slate-200 text-slate-500 hover:text-slate-700 inline-flex items-center gap-1 text-[10.5px] transition-colors"
                >
                    <MoreHorizontalIcon className="w-3 h-3" />
                    {showQuoted ? "Hide quoted text" : "Show quoted text"}
                </button>
            )}
        </>
    );
}
