// Round-tripping an email passage between the TipTap body and a plain-text
// model (issue #432).
//
// A campaign body is not plain text: merge variables, per-recipient AI blocks,
// conditionals and form links are atom nodes. ProseMirror's own doc.textBetween
// returns nothing for an atom, so the passage handed to the model arrived with
// every token silently deleted, and the rewrite came back with them gone. The
// passage is read through TipTap's text serializers instead (each chip's
// renderText, the same path editor.getText() uses) and re-chipped on the way
// back.
//
// Links are the other thing a plain-text trip loses, and losing one is worse
// than losing a word: a hand-placed {{.UnsubscribeLink}} anchor simply stopped
// existing after a rewrite. They travel as markdown, which the model handles
// natively, and a destination it mangles lands as visible text rather than a
// quietly wrong href. Bold, italic and headings inside the selection are NOT
// preserved: they have no plain-text form that survives a rewrite, and the docs
// say so rather than the code pretending otherwise.
//
// The replacement goes in with paste semantics, replaceRange over a parsed
// slice, rather than insertContentAt, which closes the slice it builds: that
// turned a rewrite of a phrase inside a sentence into three paragraphs.

import { elementFromString, getTextSerializersFromSchema } from "@tiptap/core";
import type { Editor } from "@tiptap/react";
import { DOMParser as PMDOMParser, type Node as PMNode, type Slice } from "@tiptap/pm/model";
import { TextSelection } from "@tiptap/pm/state";
import { encodeConfig, type AIVariableConfig } from "@/lib/aiVariables";
import { FIELD_TOKEN_RE, FORM_LINK_RE } from "@/lib/templateVars";

const AI_TOKEN_SOURCE = "\\[\\[ai:[A-Za-z0-9_-]{1,64}\\]\\]";
// A markdown link, as passageText writes one. The destination may be a merge
// token ({{.UnsubscribeLink}}), so it is anything without a space or a paren,
// or anything at all inside angle brackets: a URL is allowed both, and a
// Wikipedia article ending in ")" is the common one.
const MD_LINK_SOURCE = "\\[[^\\]\\n]*\\]\\((?:<[^>\\n]*>|[^)\\s]+)\\)";
const CONDITIONAL_SOURCE = "\\{\\{\\s*if\\s[\\s\\S]*?\\{\\{\\s*end\\s*\\}\\}";

// One scan over the model's text, widest structure first: a conditional wraps
// its own then/else copy, so it has to match before the field tokens inside it.
// Built per scan, never shared: a link's label is scanned inside the outer
// scan, and a /g regex carries a lastIndex the nested call would reset.
const TOKEN_SOURCE = [
    CONDITIONAL_SOURCE,
    AI_TOKEN_SOURCE,
    MD_LINK_SOURCE,
    FORM_LINK_RE.source,
    FIELD_TOKEN_RE.source,
].join("|");

const AI_TOKEN_RE = /^\[\[ai:([A-Za-z0-9_-]{1,64})\]\]$/;
const MD_LINK_RE = /^\[([^\]\n]*)\]\((?:<([^>\n]*)>|([^)\s]+))\)$/;
// Only a destination that is actually a destination becomes an anchor, so a
// "[note](see below)" the author wrote stays the text they wrote.
const HREF_RE = /^(https?:\/\/|mailto:|tel:|\{\{)/i;
const FORM_TOKEN_RE = /^\{\{\s*form_link:([a-z0-9]{1,64})\s*\}\}$/;

function escapeHTML(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function escapeAttr(s: string): string {
    return escapeHTML(s).replace(/"/g, "&quot;");
}

function plainHTML(s: string): string {
    return escapeHTML(s).replace(/\n/g, "<br>");
}

// A token becomes the span shape its node parses, matching how the chips
// serialize themselves. Every parseHTML here refuses a span it cannot read, so
// a malformed token degrades to plain text rather than an empty chip.
function chipHTML(token: string, aiConfigs: Map<string, string>): string {
    const md = token.match(MD_LINK_RE);
    if (md) {
        const href = md[2] ?? md[3] ?? "";
        if (!HREF_RE.test(href)) return plainHTML(token);
        // The label is copy like any other, so the tokens inside it are chipped
        // too. It cannot hold another link: the pattern stops at a "]".
        return `<a href="${escapeAttr(href)}">${inlineHTML(md[1], aiConfigs)}</a>`;
    }
    const ai = token.match(AI_TOKEN_RE);
    if (ai) {
        const config = aiConfigs.get(ai[1]);
        // A block id the passage never carried has no config to restore, and an
        // AI block with no prompt generates nothing, so it stays literal text.
        if (!config) return plainHTML(token);
        return `<span data-ai-var="${escapeAttr(ai[1])}" data-ai-config="${escapeAttr(config)}">${escapeHTML(token)}</span>`;
    }
    const form = token.match(FORM_TOKEN_RE);
    if (form) return `<span data-form-link="${escapeAttr(form[1])}">${escapeHTML(token)}</span>`;
    if (/^\{\{\s*if\s/.test(token)) return `<span data-if="">${escapeHTML(token)}</span>`;
    return `<span data-var="">${escapeHTML(token)}</span>`;
}

function inlineHTML(text: string, aiConfigs: Map<string, string>): string {
    const scan = new RegExp(TOKEN_SOURCE, "g");
    let out = "";
    let last = 0;
    for (let m = scan.exec(text); m; m = scan.exec(text)) {
        out += plainHTML(text.slice(last, m.index));
        out += chipHTML(m[0], aiConfigs);
        last = m.index + m[0].length;
    }
    return out + plainHTML(text.slice(last));
}

// The backend refuses a context longer than its own cap, which would turn a
// valid rewrite of a long body into an error. Tone context is advisory, so it
// is trimmed rather than allowed to fail the call. The cap is in characters on
// both sides (the server counts runes), and the trim is by code point: slicing
// UTF-16 units can cut an emoji in half and send a lone surrogate.
const CONTEXT_LIMIT = 6000;

export function clampContext(text: string): string {
    const points = Array.from(text);
    return points.length <= CONTEXT_LIMIT ? text : points.slice(0, CONTEXT_LIMIT).join("");
}

// The model returns its answer trimmed, so an edit of a selection that started
// or ended on a space would eat that space and glue the rewrite to the word
// next to it. The author's edges are put back, which also makes "did anything
// change?" an exact comparison rather than a trimmed one.
export function restoreEdges(original: string, edited: string): string {
    const lead = /^\s*/.exec(original)?.[0] ?? "";
    const trail = /\s*$/.exec(original)?.[0] ?? "";
    return original.trim() === "" ? original : lead + edited.trim() + trail;
}

function linkHref(node: PMNode): string {
    return String(node.marks.find((m) => m.type.name === "link")?.attrs.href ?? "");
}

// A link is only written as markdown when the markdown reads back as the same
// link; anything else would come back as literal brackets in the body, which is
// worse than an unmarked run of words. A destination carrying a ")" or a space
// goes in angle brackets, the markdown form for exactly that.
function markdownLink(label: string, href: string): string {
    if (label.includes("]") || label.includes("\n") || !href) return label;
    if (!/[)\s<>]/.test(href)) return `[${label}](${href})`;
    if (href.includes(">") || href.includes("\n")) return label;
    return `[${label}](<${href}>)`;
}

// passageText reads a document range as the model should see it: chips as their
// literal tokens, links as markdown, a blank line between blocks so plain text
// and editor HTML agree on where a paragraph ends.
//
// This is getTextBetween's walk with link marks folded in; the helper itself
// only sees nodes, and a link is a mark over a run of them.
export function passageText(editor: Editor, from: number, to: number): string {
    const serializers = getTextSerializersFromSchema(editor.schema);
    const range = { from, to };
    let out = "";
    // The link run being accumulated, so one anchor is one [text](href).
    let open: { href: string; label: string } | null = null;
    const closeLink = () => {
        if (open) out += markdownLink(open.label, open.href);
        open = null;
    };

    editor.state.doc.nodesBetween(from, to, (node, pos, parent, index) => {
        if (node.isBlock && pos > from) {
            closeLink();
            out += "\n\n";
        }
        const serializer = serializers[node.type.name];
        if (serializer) {
            if (parent) {
                closeLink();
                out += serializer({ node, pos, parent, index, range });
            }
            return false;
        }
        if (!node.isText) return;
        const text = node.text?.slice(Math.max(from, pos) - pos, to - pos) ?? "";
        const href = linkHref(node);
        if (href && open?.href === href) {
            open.label += text;
            return;
        }
        closeLink();
        if (href) open = { href, label: text };
        else out += text;
    });
    closeLink();
    return out;
}

// passageAIConfigs maps the AI blocks inside a range to their encoded config, so
// a token the model hands back is restored as the same block rather than an
// empty one.
export function passageAIConfigs(editor: Editor, from: number, to: number): Map<string, string> {
    const out = new Map<string, string>();
    editor.state.doc.nodesBetween(from, to, (node) => {
        if (node.type.name !== "aiVariable") return;
        const id = String(node.attrs.id ?? "");
        if (id) out.set(id, encodeConfig(node.attrs.config as AIVariableConfig));
    });
    return out;
}

// passageHTML turns model text back into editor HTML: a blank line starts a
// paragraph, a single newline is a line break, and every token becomes its chip.
//
// The split consumes ONE separator per break, the exact inverse of the "\n\n"
// passageText joins blocks with, so an empty paragraph between two others comes
// back as an empty paragraph instead of being swallowed by a greedy \n{2,}. An
// odd newline left over at a block's edge is the model's own spacing noise and
// is dropped rather than rendered as a stray break.
export function passageHTML(text: string, aiConfigs?: Map<string, string>): string {
    const configs = aiConfigs ?? new Map<string, string>();
    return text
        .split("\n\n")
        .map((block) => block.replace(/^\n+|\n+$/g, ""))
        .map((block) => `<p>${inlineHTML(block, configs) || "<br>"}</p>`)
        .join("");
}

function parseSlice(editor: Editor, html: string): Slice {
    return PMDOMParser.fromSchema(editor.schema).parseSlice(elementFromString(html), {
        preserveWhitespace: "full",
    });
}

// replacePassage swaps a range for parsed HTML the way a paste would. A
// replacement is left selected, for review; an insertion at a caret leaves the
// caret after what was written, so the next keystroke continues it. Returns the
// end of the new content, or null when the document did not change — a rewrite
// that changed nothing must not be reported as one that did.
export function replacePassage(editor: Editor, from: number, to: number, html: string): number | null {
    const inserting = from === to;
    const tr = editor.state.tr.replaceRange(from, to, parseSlice(editor, html));
    // docChanged only says a step ran: replacing a passage with the same words
    // still produces one. The documents themselves are compared instead.
    if (tr.doc.eq(editor.state.doc)) return null;
    // An inserted-at position maps to itself unless it associates rightwards,
    // which would leave the caret in front of the text just written.
    const end = tr.mapping.map(to, inserting ? 1 : -1);
    const $end = tr.doc.resolve(end);
    tr.setSelection(inserting ? TextSelection.near($end, -1) : TextSelection.between(tr.doc.resolve(Math.min(from, end)), $end));
    editor.view.dispatch(tr.scrollIntoView());
    return end;
}
