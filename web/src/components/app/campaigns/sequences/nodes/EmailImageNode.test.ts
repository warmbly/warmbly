// A body image, and the anchor it can wear (issues #380, #433).
//
// ProseMirror cannot put the link mark on an image — a block leaf takes no
// marks — so the anchor is part of what the node renders and is read back off
// the parent <a>. That round trip is the whole feature: an href the editor
// writes but cannot parse is a link that disappears when the step is reopened.

import { describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import Document from "@tiptap/extension-document";
import Text from "@tiptap/extension-text";
import Link from "@tiptap/extension-link";
import { emailDesignExtensions, EmailParagraph } from "./emailHtml";
import { absoluteHref, EmailImage } from "./EmailImageNode";

const extensions = [
    Document,
    EmailParagraph,
    Text,
    Link.configure({ openOnClick: false, autolink: true }),
    EmailImage,
    ...emailDesignExtensions,
];

function editorWith(html: string): Editor {
    return new Editor({ element: document.createElement("div"), extensions, content: html });
}

function roundTrip(html: string): string {
    const editor = editorWith(html);
    const out = editor.getHTML();
    editor.destroy();
    return out;
}

function imageAttrs(html: string): Record<string, unknown> | null {
    const editor = editorWith(html);
    let found: Record<string, unknown> | null = null;
    editor.state.doc.descendants((node) => {
        if (node.type.name === "image") found = node.attrs;
    });
    editor.destroy();
    return found;
}

const SRC = "https://cdn.test/logo.png";

// jsdom reflects the style attribute through the CSSOM and re-serializes it,
// so a length written as `0` comes back as `0px`. A browser keeps what was
// written, hence the tolerance rather than a literal.
const margin = (sides: string) => new RegExp(`margin:\\s*${sides.replace(/ /g, "(?:px)?\\s+")}(?:px)?`);

describe("a body image", () => {
    it("renders bare when it links nowhere", () => {
        const out = roundTrip(`<img src="${SRC}" alt="Logo">`);
        expect(out).toContain("<img");
        expect(out).not.toContain("<a");
        expect(out).toMatch(margin("0 auto 0 0"));
    });

    it("wraps itself in the anchor it was given, and reads it back", () => {
        const out = roundTrip(
            `<a href="https://warmbly.test/demo"><img src="${SRC}" alt="Logo" width="300" data-align="center"></a>`,
        );
        expect(out).toContain('<a href="https://warmbly.test/demo"');
        expect(out).toContain('target="_blank"');
        expect(out).toContain('rel="noopener noreferrer nofollow"');
        expect(roundTrip(out)).toBe(out);
        expect(imageAttrs(out)).toMatchObject({ href: "https://warmbly.test/demo", width: 300, align: "center" });
    });

    it("gives the anchor the image's own box, so the click area is the picture", () => {
        const out = roundTrip(`<a href="https://x.test"><img src="${SRC}" width="300" data-align="center"></a>`);
        const anchor = out.slice(out.indexOf("<a"), out.indexOf("<img"));
        expect(anchor).toContain("width: 300px");
        expect(anchor).toMatch(/margin:\s*0(?:px)?\s+auto/);
        // And on the image as well: inside a box it already fills that is a
        // no-op, and it is the only centring left in a client that ignores
        // display:block on an anchor.
        expect(out.slice(out.indexOf("<img"))).toMatch(/margin:\s*0(?:px)?\s+auto/);
    });

    it("keeps the margins on the image when it has no width to hand over", () => {
        const out = roundTrip(`<a href="https://x.test"><img src="${SRC}" data-align="right"></a>`);
        expect(out.slice(out.indexOf("<img"))).toMatch(margin("0 0 0 auto"));
    });

    it("refuses an address no mail client would follow", () => {
        const out = roundTrip(`<a href="javascript:alert(1)"><img src="${SRC}" alt="Logo"></a>`);
        expect(out).not.toContain("<a");
        expect(out).not.toContain("javascript");
    });

    it("takes an href only from the anchor wrapping it", () => {
        // A template that links a whole layout block must not hand its address
        // to every picture inside it.
        const attrs = imageAttrs(
            `<table><tr><td><a href="https://x.test"><p>Read on</p></a></td></tr></table><img src="${SRC}">`,
        );
        expect(attrs).toMatchObject({ href: null });
    });
});

describe("the address someone typed", () => {
    it("becomes the one they meant", () => {
        expect(absoluteHref("cal.test/me")).toBe("https://cal.test/me");
        expect(absoluteHref(" warmbly.com ")).toBe("https://warmbly.com");
    });

    it("leaves alone anything that already says what it is", () => {
        expect(absoluteHref("https://x.test")).toBe("https://x.test");
        expect(absoluteHref("mailto:sam@x.test")).toBe("mailto:sam@x.test");
        expect(absoluteHref("//cdn.test/a")).toBe("//cdn.test/a");
        expect(absoluteHref("{{.UnsubscribeLink}}")).toBe("{{.UnsubscribeLink}}");
        expect(absoluteHref("/pricing")).toBe("/pricing");
        expect(absoluteHref("")).toBe("");
    });

    it("does not guess at something that is not an address yet", () => {
        expect(absoluteHref("cal")).toBe("cal");
    });
});
