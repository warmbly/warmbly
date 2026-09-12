// The call-to-action button for the campaign body editor (issue #433).
//
// A button in email is not a <button>: no client renders one, and every client
// strips it. It is an anchor a table is holding up. Outlook on Windows lays out
// with Word, which ignores `display:inline-block` outright, so padding on the
// anchor collapses and a styled <a> becomes a coloured word. Putting the colour
// and the padding on a one-cell table instead is the shape that holds
// everywhere, which is why this renders markup rather than a class.
//
// The node is an atom: the label is an attribute edited in the floating bar,
// the same way an image's alt text is. Merge fields and spintax in the label
// are plain text by the time the send path reads the body, so they still work.

import { Node } from "@tiptap/core";
import { NodeSelection } from "@tiptap/pm/state";
import { MAIL_LINK_ATTRS, mailHref } from "./EmailImageNode";

export type ButtonAlign = "left" | "center" | "right";
export type ButtonSize = "sm" | "md" | "lg";

// Padding on the cell and type size on the anchor, as one choice. Both are
// stored as they were written rather than as the name of a preset, so a value
// tuned by hand in the HTML view survives coming back to the visual editor.
export const BUTTON_SIZES: { key: ButtonSize; label: string; title: string; padding: string; fontSize: number }[] = [
    { key: "sm", label: "S", title: "Small", padding: "8px 16px", fontSize: 13 },
    { key: "md", label: "M", title: "Medium", padding: "12px 24px", fontSize: 14 },
    { key: "lg", label: "L", title: "Large", padding: "16px 32px", fontSize: 16 },
];

// A pill is 999px rather than 50%: Outlook resolves a percentage radius
// against the wrong box and squares the corners off. Stored as the CSS value
// rather than a number, so a corner rounded on one side only in the HTML view
// is not squared off on the way back.
export const BUTTON_RADII: { label: string; title: string; value: string }[] = [
    { label: "Square", title: "Square corners", value: "0" },
    { label: "Rounded", title: "Rounded corners", value: "6px" },
    { label: "Pill", title: "Fully rounded", value: "999px" },
];

const BUTTON_DEFAULT_RADIUS = "6px";

export const BUTTON_DEFAULT_LABEL = "Book a call";
export const BUTTON_DEFAULT_BACKGROUND = "#0284c7";

// The font stack is written out because a button is the one place in a body
// that must not inherit a template's decorative face and fall back at random.
const BUTTON_FONT = "Arial, Helvetica, sans-serif";

// readableTextColor picks the label colour the background can carry, so a
// yellow button is not white-on-yellow. WCAG relative luminance, with the
// threshold where white stops being the better of the two.
export function readableTextColor(background: string): string {
    const hex = background.trim().replace(/^#/, "");
    const full = hex.length === 3 ? [...hex].map((c) => c + c).join("") : hex;
    if (!/^[0-9a-f]{6}$/i.test(full)) return "#ffffff";
    const channel = (at: number) => {
        const c = Number.parseInt(full.slice(at, at + 2), 16) / 255;
        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    };
    const luminance = 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4);
    return luminance > 0.45 ? "#0f172a" : "#ffffff";
}

// decl reads one declaration off an element's own style attribute. The DOM
// would answer too, but it normalises a colour to rgb() on the way, and the
// hex the author picked is what has to come back.
function decl(el: Element | null | undefined, prop: string): string {
    const raw = el?.getAttribute("style") ?? "";
    const match = new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`, "i").exec(raw);
    return match ? match[1].trim() : "";
}

// marginFor is the button's placement and its breathing room, written out
// because the editing surface has to show the same box the email will: a
// stylesheet margin here would be one the recipient never gets.
function marginFor(align: ButtonAlign): string {
    const left = align === "left" ? "0" : "auto";
    const right = align === "right" ? "0" : "auto";
    return `margin:16px ${right} 16px ${left}`;
}

export const EmailButton = Node.create({
    name: "emailButton",
    group: "block",
    atom: true,
    draggable: true,

    addAttributes() {
        return {
            label: { default: BUTTON_DEFAULT_LABEL },
            href: { default: "" },
            background: { default: BUTTON_DEFAULT_BACKGROUND },
            color: { default: readableTextColor(BUTTON_DEFAULT_BACKGROUND) },
            padding: { default: BUTTON_SIZES[1].padding },
            fontSize: { default: BUTTON_SIZES[1].fontSize },
            radius: { default: BUTTON_DEFAULT_RADIUS },
            align: { default: "center" as ButtonAlign },
            // "100%" is the full-column button a phone gets pressed on. Any
            // other width is one somebody wrote themselves, and it is kept.
            width: { default: null as string | null },
        };
    },

    parseHTML() {
        return [
            {
                // Above the layout-table rule, which would otherwise claim this
                // markup first and leave a two-cell table nobody can edit.
                priority: 100,
                tag: "table[data-warmbly-button]",
                getAttrs: (element) => {
                    const table = element as HTMLElement;
                    const cell = table.querySelector("td");
                    const anchor = table.querySelector("a");
                    const background =
                        cell?.getAttribute("bgcolor") || decl(cell, "background-color") || BUTTON_DEFAULT_BACKGROUND;
                    const fontSize = Number.parseInt(decl(anchor, "font-size"), 10);
                    const declaredAlign = (table.getAttribute("align") ?? "").toLowerCase();
                    return {
                        label: anchor?.textContent?.trim() || BUTTON_DEFAULT_LABEL,
                        href: anchor?.getAttribute("href") ?? "",
                        background,
                        color: decl(anchor, "color") || readableTextColor(background),
                        padding: decl(cell, "padding") || BUTTON_SIZES[1].padding,
                        fontSize: Number.isFinite(fontSize) ? fontSize : BUTTON_SIZES[1].fontSize,
                        radius: decl(cell, "border-radius") || BUTTON_DEFAULT_RADIUS,
                        align: declaredAlign === "left" || declaredAlign === "right" ? declaredAlign : "center",
                        width: table.getAttribute("width"),
                    };
                },
            },
        ];
    },

    renderHTML({ node }) {
        const { label, href, background, color, padding, fontSize, radius, width } = node.attrs as {
            label: string;
            href: string;
            background: string;
            color: string;
            padding: string;
            fontSize: number;
            radius: string;
            width: string | null;
        };
        const align = (node.attrs.align as ButtonAlign) ?? "center";

        const table: Record<string, string> = {
            "data-warmbly-button": "",
            role: "presentation",
            cellpadding: "0",
            cellspacing: "0",
            border: "0",
            align,
            style: `border-collapse:separate;${width ? `width:${width};` : ""}${marginFor(align)}`,
        };
        if (width) table.width = width;

        const cell = {
            align: "center",
            // bgcolor as well as the style: Word reads the attribute, and a
            // button that loses its colour there is just underlined text.
            bgcolor: background,
            style: `background-color:${background};border-radius:${radius};padding:${padding};text-align:center`,
        };

        const anchor: Record<string, string> = {
            style:
                `display:inline-block;color:${color};font-family:${BUTTON_FONT};` +
                `font-size:${fontSize}px;font-weight:600;line-height:1.2;text-decoration:none`,
        };
        // An empty href is a dead link in every client, so a button without one
        // ships as the box it looks like and nothing more.
        const address = mailHref(href);
        if (address) Object.assign(anchor, { href: address, ...MAIL_LINK_ATTRS });

        return ["table", table, ["tbody", ["tr", ["td", cell, ["a", anchor, label]]]]];
    },

    addCommands() {
        const name = this.name;
        return {
            insertEmailButton:
                (attrs = {}) =>
                ({ chain, state }) =>
                    chain()
                        // After the selection rather than over it: the toolbar
                        // is reachable while a button is selected, which is
                        // exactly when its bar is open, and pressing insert
                        // there means "another one", not "throw this one away".
                        .insertContentAt(state.selection.to, { type: name, attrs })
                        // Select what was just placed, so the bar that edits it
                        // opens without a second click. It is the last button
                        // at or before the caret, which after an insert is the
                        // new one.
                        .command(({ tr, dispatch }) => {
                            let at: number | null = null;
                            tr.doc.nodesBetween(0, tr.selection.from, (node, pos) => {
                                if (node.type.name === name) at = pos;
                            });
                            if (at !== null && dispatch) tr.setSelection(NodeSelection.create(tr.doc, at));
                            return true;
                        })
                        .run(),
        };
    },
});

declare module "@tiptap/core" {
    interface Commands<ReturnType> {
        emailButton: {
            /** Place a call-to-action button at the caret. */
            insertEmailButton: (attrs?: Record<string, unknown>) => ReturnType;
        };
    }
}

export default EmailButton;
