// The call-to-action button, through the editor that writes it (issue #433).
//
// A button is markup, not a class: what it renders IS what lands in the
// recipient's client, and what it parses back is what an author sees when they
// reopen the step. Both halves are asserted here, because a button that comes
// back as a plain layout table is a design the editor quietly destroyed.

import { describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { NodeSelection } from "@tiptap/pm/state";
import Document from "@tiptap/extension-document";
import Text from "@tiptap/extension-text";
import Link from "@tiptap/extension-link";
import { emailDesignExtensions, EmailParagraph } from "./emailHtml";
import { EmailButton, readableTextColor } from "./EmailButtonNode";

const extensions = [
    Document,
    EmailParagraph,
    Text,
    Link.configure({ openOnClick: false, autolink: true }),
    EmailButton,
    // The table extensions are mounted here too, because the one thing that
    // could break parsing is the layout-table rule claiming this markup first.
    ...emailDesignExtensions,
];

function editorWith(html: string): Editor {
    return new Editor({ element: document.createElement("div"), extensions, content: html });
}

function render(attrs: Record<string, unknown>): string {
    const editor = editorWith("<p>x</p>");
    editor.commands.insertEmailButton(attrs);
    const out = editor.getHTML();
    editor.destroy();
    return out;
}

function roundTrip(html: string): string {
    const editor = editorWith(html);
    const out = editor.getHTML();
    editor.destroy();
    return out;
}

function buttonAttrs(html: string): Record<string, unknown> | null {
    const editor = editorWith(html);
    let found: Record<string, unknown> | null = null;
    editor.state.doc.descendants((node) => {
        if (node.type.name === "emailButton") found = node.attrs;
    });
    editor.destroy();
    return found;
}

describe("the call-to-action button", () => {
    it("renders a one-cell table carrying the colour and the padding", () => {
        const out = render({ label: "Book a call", href: "https://cal.test/me" });
        // The cell, not the anchor, holds both: Outlook lays out with Word,
        // which ignores display:inline-block and drops the anchor's padding.
        expect(out).toContain('bgcolor="#0284c7"');
        expect(out).toContain("padding: 12px 24px");
        expect(out).toContain("border-radius: 6px");
        expect(out).toContain('href="https://cal.test/me"');
        expect(out).toContain('target="_blank"');
        expect(out).toContain('rel="noopener noreferrer nofollow"');
        expect(out).toContain(">Book a call</a>");
    });

    it("comes back exactly as it went out", () => {
        const html = render({
            label: "See the pricing",
            href: "https://warmbly.test/pricing",
            background: "#e11d48",
            color: "#ffffff",
            padding: "16px 32px",
            fontSize: 16,
            radius: "999px",
            align: "left",
        });
        expect(roundTrip(html)).toBe(html);

        const attrs = buttonAttrs(html);
        expect(attrs).toMatchObject({
            label: "See the pricing",
            href: "https://warmbly.test/pricing",
            background: "#e11d48",
            padding: "16px 32px",
            fontSize: 16,
            radius: "999px",
            align: "left",
            width: null,
        });
    });

    it("keeps a size somebody tuned by hand in the HTML view", () => {
        // The presets set the padding and the type size; they are not the only
        // two values allowed, or editing the markup would silently undo itself.
        const attrs = buttonAttrs(
            '<table data-warmbly-button="" align="center"><tbody><tr>' +
                '<td bgcolor="#0284c7" style="border-radius:4px;padding:10px 30px">' +
                '<a href="https://x.test" style="color:#ffffff;font-size:15px">Talk to us</a>' +
                "</td></tr></tbody></table>",
        );
        expect(attrs).toMatchObject({ padding: "10px 30px", fontSize: 15, radius: "4px", label: "Talk to us" });
    });

    it("is not claimed by the layout-table rule", () => {
        const editor = editorWith(render({ label: "Reply", href: "https://x.test" }));
        const names: string[] = [];
        editor.state.doc.descendants((node) => {
            names.push(node.type.name);
        });
        editor.destroy();
        expect(names).toContain("emailButton");
        expect(names).not.toContain("table");
    });

    it("fills the column on request, and sizes to its label otherwise", () => {
        expect(render({ href: "https://x.test", width: "100%" })).toContain('width="100%"');
        expect(render({ href: "https://x.test" })).not.toContain('width="100%"');
    });

    it("ships no anchor target when it has no link", () => {
        const out = render({ label: "Nowhere", href: "" });
        // An empty href is a dead link in every client; better a box that does
        // nothing than one that navigates to the message itself.
        expect(out).not.toContain("href=");
        expect(out).toContain(">Nowhere</a>");
    });

    it("centres by attribute as well as by margin", () => {
        // Word reads align on the table and nothing else, so the margin alone
        // would leave the button hard left for every Outlook on Windows.
        const out = render({ href: "https://x.test", align: "center" });
        expect(out).toContain('align="center"');
        // jsdom collapses the four-value margin the node writes; a browser
        // keeps it as written, so the assertion allows both.
        expect(out).toMatch(/margin:\s*16px auto(?: 16px auto)?/);
    });

    it("selects what it just inserted, so the bar that edits it is already open", () => {
        const editor = editorWith("<p>Hi</p>");
        editor.commands.insertEmailButton({ label: "Book a call" });
        const selection = editor.state.selection;
        expect(selection).toBeInstanceOf(NodeSelection);
        expect((selection as NodeSelection).node.type.name).toBe("emailButton");
        editor.destroy();
    });

    it("adds a second button rather than replacing the selected one", () => {
        // The toolbar is reachable while a button is selected, which is exactly
        // when its bar is open: pressing insert there must not throw away the
        // label, link and colour someone just set.
        const editor = editorWith("<p>Hi</p>");
        editor.commands.insertEmailButton({ label: "First", href: "https://a.test" });
        editor.commands.insertEmailButton({ label: "Second", href: "https://b.test" });
        const labels: string[] = [];
        editor.state.doc.descendants((node) => {
            if (node.type.name === "emailButton") labels.push(node.attrs.label as string);
        });
        editor.destroy();
        expect(labels).toEqual(["First", "Second"]);
    });

    it("keeps a corner rounded on one side only", () => {
        const attrs = buttonAttrs(
            '<table data-warmbly-button=""><tbody><tr><td style="border-radius:12px 12px 0 0">' +
                '<a href="https://x.test">Top</a></td></tr></tbody></table>',
        );
        expect(attrs).toMatchObject({ radius: "12px 12px 0 0" });
    });

    it("refuses an address no mail client would follow", () => {
        // The link mark blocks these, so the one place a URL is typed by hand
        // must not be the one that lets them through.
        for (const href of ["javascript:alert(1)", "java\tscript:alert(1)", "data:text/html,<b>x</b>"]) {
            expect(render({ label: "Go", href })).not.toContain("href=");
        }
    });

    it("picks a label colour the background can carry", () => {
        expect(readableTextColor("#0f172a")).toBe("#ffffff");
        expect(readableTextColor("#d97706")).toBe("#ffffff");
        expect(readableTextColor("#fde047")).toBe("#0f172a");
        expect(readableTextColor("nonsense")).toBe("#ffffff");
    });
});
