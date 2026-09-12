// The <img> node for the campaign body editor (issue #380).
//
// Mail clients ignore stylesheets and half of them ignore <style> blocks too,
// so every layout decision has to survive as an inline style or an attribute on
// the tag itself. Width is written as both (Outlook reads the attribute), and
// alignment as auto margins on a block image, which is the one centring trick
// every client honours.
//
// An image can also be a link (issue #433). ProseMirror cannot put the link
// mark on it — a block leaf takes no marks — so the anchor is part of what this
// node renders, and read back off the parent <a> on the way in.

import { mergeAttributes } from "@tiptap/core";
import Image from "@tiptap/extension-image";

export type ImageAlign = "left" | "center" | "right";

// The width a body renders at in most mail clients; the size presets are
// fractions of it, so "L" fills the column instead of overflowing it.
export const EMAIL_BODY_WIDTH = 600;

export const IMAGE_SIZE_PRESETS: { label: string; title: string; width: number | null }[] = [
    { label: "S", title: "Quarter width", width: Math.round(EMAIL_BODY_WIDTH * 0.25) },
    { label: "M", title: "Half width", width: Math.round(EMAIL_BODY_WIDTH * 0.5) },
    { label: "L", title: "Full width", width: EMAIL_BODY_WIDTH },
    { label: "Auto", title: "The image's own size", width: null },
];

// What every anchor this editor writes carries, matching the link mark's own
// defaults so a linked image and a linked word behave the same in a webmail.
export const MAIL_LINK_ATTRS = { target: "_blank", rel: "noopener noreferrer nofollow" };

// Schemes that do nothing in a mail client and are an attack anywhere else the
// body is rendered. The link mark refuses them, so an image and a button have
// to as well, or the one place a URL is typed by hand is the one that does not.
const UNSAFE_SCHEME = /^(javascript|data|vbscript|file):/i;

// mailHref is the address an anchor may actually carry: unsafe schemes become
// nothing at all, and everything else is written as given.
export function mailHref(raw: string): string {
    const href = raw.trim();
    // Characters a URL parser ignores come out before the scheme is read:
    // "java<tab>script:" is a javascript: URL to a browser, and is not one to
    // a regular expression.
    const bare = [...href].filter((c) => c.charCodeAt(0) > 0x20).join("");
    return UNSAFE_SCHEME.test(bare) ? "" : href;
}

// absoluteHref upgrades what someone typed into what they meant: a bare host is
// a relative path to every mail client, which then goes nowhere, is never
// counted as a click, and is left out of the plain-text half. Anything already
// carrying a scheme, a merge token, or a path is left exactly as written.
export function absoluteHref(raw: string): string {
    const href = raw.trim();
    if (!href || /^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("//") || href.includes("{{")) return href;
    return /^[^\s/]+\.[^\s/]/.test(href) ? `https://${href}` : href;
}

function readWidth(el: HTMLElement): number | null {
    const raw = el.getAttribute("width") || el.style.width || "";
    const n = Number.parseInt(raw, 10);
    return Number.isFinite(n) && n > 0 ? n : null;
}

// The anchor an image is wrapped in, and only the one wrapping it directly:
// a template that links a whole layout block would otherwise hand its href to
// every picture inside it.
function readHref(el: HTMLElement): string | null {
    const parent = el.parentElement;
    if (!parent || parent.tagName !== "A") return null;
    return parent.getAttribute("href") || null;
}

// marginFor is the alignment rule: auto margins on a block, which is the one
// centring trick every mail client honours.
function marginFor(align: ImageAlign): string {
    if (align === "center") return "margin:0 auto";
    return align === "right" ? "margin:0 0 0 auto" : "margin:0 auto 0 0";
}

export const EmailImage = Image.extend({
    addAttributes() {
        return {
            ...this.parent?.(),
            width: {
                default: null,
                parseHTML: (el) => readWidth(el as HTMLElement),
                // Composed into the tag's style + width by renderHTML below.
                renderHTML: () => ({}),
            },
            // A stale height fights `height:auto` when a client scales the
            // image down to the screen, so it is never carried.
            height: {
                default: null,
                parseHTML: () => null,
                renderHTML: () => ({}),
            },
            align: {
                default: "left" as ImageAlign,
                parseHTML: (el) => {
                    const a = (el as HTMLElement).getAttribute("data-align");
                    return a === "center" || a === "right" ? a : "left";
                },
                renderHTML: (attrs) => ({ "data-align": (attrs.align as ImageAlign) ?? "left" }),
            },
            href: {
                default: null as string | null,
                parseHTML: (el) => readHref(el as HTMLElement),
                // Rendered as the wrapping anchor, never as an attribute.
                renderHTML: () => ({}),
            },
        };
    },

    renderHTML({ node, HTMLAttributes }) {
        const width = typeof node.attrs.width === "number" ? node.attrs.width : null;
        const align = (node.attrs.align as ImageAlign) ?? "left";
        const href = typeof node.attrs.href === "string" ? mailHref(node.attrs.href) : "";
        const style = ["display:block", "max-width:100%", "height:auto", "border:0"];
        if (width) style.push(`width:${width}px`);

        const extra: Record<string, string> = {};
        if (width) extra.width = String(width);
        if (!href) {
            extra.style = [...style, marginFor(align)].join(";");
            return ["img", mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, extra)];
        }

        // A known width lets the anchor take the image's own box, so the
        // clickable area is the picture rather than the whole row, and the
        // alignment goes there with it. It stays on the image too: inside a box
        // the image already fills that is a no-op, and in a client that ignores
        // display:block on an anchor it is the only centring left.
        const wrapper = ["display:block", "text-decoration:none", "border:0"];
        if (width) wrapper.push(`width:${width}px`, marginFor(align));
        extra.style = [...style, marginFor(align)].join(";");
        return [
            "a",
            { href, ...MAIL_LINK_ATTRS, style: wrapper.join(";") },
            ["img", mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, extra)],
        ];
    },
});

export default EmailImage;
