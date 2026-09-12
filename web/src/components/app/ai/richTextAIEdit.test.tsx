// Issue #432: "Edit with AI" reported a rewrite the body never showed.
//
// Three separate defects produced that, and each one is pinned here against
// the real campaign body editor rather than a stand-in, because every one of
// them lived in how the passage crossed between ProseMirror and a plain-text
// model:
//
//   - the passage handed to the model was doc.textBetween, which returns
//     nothing for an atom node, so every merge variable was deleted on the way
//     out and the rewrite came back with them gone
//   - paragraphs were separated by one newline on the way out and split on a
//     blank line on the way back, so a multi-paragraph selection collapsed
//     into one
//   - the replacement went in through insertContentAt, which closes the slice
//     it parses, so rewriting a phrase inside a sentence split the paragraph
//     into three
//
// Plus the review row claiming "Rewritten" for a model that returned the
// passage untouched.

import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Editor } from "@tiptap/react";
import { encodeConfig } from "@/lib/aiVariables";

// The host owns the editor, so the test reaches it by wrapping useEditor.
let captured: Editor | null = null;
vi.mock("@tiptap/react", async (orig) => {
    const actual = (await orig()) as Record<string, unknown> & {
        useEditor: (options: unknown, deps?: unknown) => Editor | null;
    };
    return {
        ...actual,
        useEditor: (options: unknown, deps?: unknown) => {
            const editor = actual.useEditor(options, deps);
            if (editor) captured = editor;
            return editor;
        },
    };
});

vi.mock("@/lib/api/client/Request", () => ({
    default: () => Promise.resolve({ data: [], pagination: { total: 0, next_cursor: null, has_more: false } }),
}));
vi.mock("@/hooks/context/confirm", () => ({
    useConfirm: () => ({ show: (_: string, onSubmit: () => void) => onSubmit() }),
}));

let reply = { text: "", credits_charged: 1, tokens_used: 1700 };
let sent: { text: string; instruction: string; context?: string } | null = null;
vi.mock("@/lib/api/hooks/app/generation/useGenerateEdit", () => ({
    default: () => ({
        isPending: false,
        mutate: (
            vars: { text: string; instruction: string; context?: string },
            opts?: { onSuccess?: (res: unknown) => void },
        ) => {
            sent = vars;
            opts?.onSuccess?.(reply);
        },
    }),
}));
let written = { text: "", credits_charged: 1, tokens_used: 10 };
vi.mock("@/lib/api/hooks/app/generation/useGenerateWrite", () => ({
    default: () => ({
        isPending: false,
        mutate: (_vars: unknown, opts?: { onSuccess?: (res: unknown) => void }) => {
            opts?.onSuccess?.(written);
        },
    }),
}));

import RichTextEditor from "@/components/app/campaigns/sequences/RichTextEditor";

// The controlled host every campaign step uses: the body lives in the parent,
// the editor reports every change back up. A rewrite the parent never hears
// about is a rewrite that is never saved.
function Body({ initial, onChange }: { initial: string; onChange: (html: string) => void }) {
    const [html, setHtml] = React.useState(initial);
    return (
        <RichTextEditor
            html={html}
            onChange={(next) => {
                setHtml(next);
                onChange(next);
            }}
            variables={["{{.FirstName}}"]}
        />
    );
}

function mountBody(initial: string) {
    const saved = { html: initial };
    render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
            <Body initial={initial} onChange={(html) => (saved.html = html)} />
        </QueryClientProvider>,
    );
    const editor = captured!;
    // jsdom has no layout, so the pill has nothing to anchor to without this.
    editor.view.coordsAtPos = () => ({ left: 10, right: 20, top: 300, bottom: 320 });
    return { editor, saved };
}

async function select(editor: Editor, from: number, to: number) {
    await act(async () => {
        editor.commands.setTextSelection({ from, to });
    });
}

async function rewrite(instruction = "make it punchier") {
    await act(async () => {
        fireEvent.mouseDown(await screen.findByText("Edit with AI"));
    });
    const input = await screen.findByPlaceholderText("Tell AI how to change it…");
    await act(async () => {
        fireEvent.change(input, { target: { value: instruction } });
        fireEvent.keyDown(input, { key: "Enter" });
    });
}

describe("Edit with AI in the campaign body", () => {
    beforeEach(() => {
        captured = null;
        sent = null;
        reply = { text: "", credits_charged: 1, tokens_used: 1700 };
    });

    it("sends the passage with its merge variables and its paragraph breaks", async () => {
        const { editor } = mountBody('<p>Hi <span data-var="">{{.FirstName}}</span>, quick one.</p><p>Saw your ads.</p>');
        reply = { text: "Rewritten.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite();
        expect(sent?.text).toBe("Hi {{.FirstName}}, quick one.\n\nSaw your ads.");
    });

    it("carries a link through the rewrite instead of flattening it to words", async () => {
        const { editor, saved } = mountBody(
            '<p>Please <a href="{{.UnsubscribeLink}}">opt out</a> if this misses.</p>',
        );
        // The model keeps the markdown it was given and rewords around it.
        reply = { text: "Or [opt out]({{.UnsubscribeLink}}) any time.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite("make it shorter");
        expect(sent?.text).toBe("Please [opt out]({{.UnsubscribeLink}}) if this misses.");
        // The anchor is still an anchor pointing at the same token; the rest of
        // the attributes are the Link extension's own defaults.
        expect(editor.getHTML()).toContain('href="{{.UnsubscribeLink}}">opt out</a>');
        expect(editor.getText()).toBe("Or opt out any time.");
        expect(saved.html).toBe(editor.getHTML());
    });

    it("wraps a destination the plain markdown form cannot hold", async () => {
        const url = "https://en.wikipedia.org/wiki/Foo_(bar)";
        const { editor } = mountBody(`<p>See <a href="${url}">the page</a>.</p>`);
        reply = { text: "unused", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite();
        expect(sent?.text).toBe(`See [the page](<${url}>).`);
    });

    it("replaces the whole selection and reports the new body upward", async () => {
        const { editor, saved } = mountBody("<p>Original one.</p><p>Original two.</p>");
        reply = { text: "New one.\n\nNew two.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite();
        expect(editor.getHTML()).toBe("<p>New one.</p><p>New two.</p>");
        expect(saved.html).toBe("<p>New one.</p><p>New two.</p>");
        await screen.findByText("Rewritten");
    });

    it("rewrites a phrase in place instead of splitting the paragraph around it", async () => {
        const { editor, saved } = mountBody("<p>One two three four.</p>");
        reply = { text: "TWO AND THREE", credits_charged: 1, tokens_used: 10 };
        await select(editor, 5, 14); // "two three"
        await rewrite();
        expect(editor.getHTML()).toBe("<p>One TWO AND THREE four.</p>");
        expect(saved.html).toBe("<p>One TWO AND THREE four.</p>");
    });

    it("brings a merge variable back as a chip, with the body value in step", async () => {
        const { editor, saved } = mountBody("<p>Hello there.</p>");
        reply = { text: "Hi {{.FirstName}}, quick one.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite();
        expect(editor.getHTML()).toBe('<p>Hi <span data-var="">{{.FirstName}}</span>, quick one.</p>');
        // The editor and the controlled value must agree, or what is saved is
        // not what is on screen.
        expect(saved.html).toBe(editor.getHTML());
    });

    it("keeps an AI block's configuration when the model echoes its token", async () => {
        const config = encodeConfig({ name: "opener", mode: "instant", prompt: "a warm opener", tone: "", web_search: false });
        const { editor } = mountBody(`<p>Hi <span data-ai-var="abc" data-ai-config="${config}">[[ai:abc]]</span> there.</p>`);
        reply = { text: "Hey [[ai:abc]] there.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        expect(sent).toBeNull();
        await rewrite();
        expect(sent?.text).toBe("Hi [[ai:abc]] there.");
        let block: { id: string; prompt: string } | null = null;
        editor.state.doc.descendants((node) => {
            if (node.type.name !== "aiVariable") return;
            block = {
                id: String(node.attrs.id),
                prompt: (node.attrs.config as { prompt: string }).prompt,
            };
        });
        expect(block).toEqual({ id: "abc", prompt: "a warm opener" });
    });

    it("says so when the model hands the passage back untouched", async () => {
        const { editor } = mountBody("<p>Already fine.</p>");
        reply = { text: "Already fine.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite("fix grammar");
        await screen.findByText("No change");
        expect(screen.queryByText("Rewritten")).toBeNull();
    });

    it("gives back the space the selection ended on", async () => {
        const { editor } = mountBody("<p>One two three four.</p>");
        // "two three " including the trailing space; the server trims its answer.
        reply = { text: "TWO AND THREE", credits_charged: 1, tokens_used: 10 };
        await select(editor, 5, 15);
        await rewrite();
        expect(editor.getHTML()).toBe("<p>One TWO AND THREE four.</p>");
    });

    it("undoes the rewrite back to the body that was there", async () => {
        const { editor, saved } = mountBody("<p>Original one.</p>");
        reply = { text: "New one.", credits_charged: 1, tokens_used: 10 };
        await select(editor, 1, editor.state.doc.content.size - 1);
        await rewrite();
        await act(async () => {
            fireEvent.click(screen.getByText("Undo"));
        });
        expect(editor.getHTML()).toBe("<p>Original one.</p>");
        expect(saved.html).toBe("<p>Original one.</p>");
    });
});

// ⌘J at the caret writes into the body through the same round trip. It went in
// with insertContentAt too, so asking for a sentence in the middle of a
// paragraph broke that paragraph in two.
describe("Write with AI at the caret", () => {
    beforeEach(() => {
        captured = null;
        written = { text: "", credits_charged: 1, tokens_used: 10 };
    });

    async function writeAtCaret(editor: Editor, pos: number, instruction: string) {
        await act(async () => {
            editor.commands.setTextSelection(pos);
            fireEvent.keyDown(editor.view.dom, { key: "j", metaKey: true });
        });
        const input = await screen.findByPlaceholderText("Ask AI to write…");
        await act(async () => {
            fireEvent.change(input, { target: { value: instruction } });
            fireEvent.keyDown(input, { key: "Enter" });
        });
    }

    it("extends the sentence it was asked to write into", async () => {
        const { editor, saved } = mountBody("<p>One four.</p>");
        written = { text: "two three", credits_charged: 1, tokens_used: 10 };
        await writeAtCaret(editor, 5, "add the middle");
        expect(editor.getHTML()).toBe("<p>One two threefour.</p>");
        expect(saved.html).toBe(editor.getHTML());
    });

    it("leaves the caret after what it wrote, not in front of it", async () => {
        const { editor } = mountBody("<p>One four.</p>");
        written = { text: "two three ", credits_charged: 1, tokens_used: 10 };
        await writeAtCaret(editor, 5, "add the middle");
        expect(editor.getHTML()).toBe("<p>One two three four.</p>");
        // Continuing to type has to continue the sentence, which it cannot do
        // from a caret parked in front of the insertion.
        expect(editor.state.selection.empty).toBe(true);
        expect(editor.state.selection.from).toBe(15);
    });

    it("lands the merge variable it was told to use as a chip", async () => {
        const { editor } = mountBody("<p>Hello.</p>");
        written = { text: "Hi {{.FirstName}}.", credits_charged: 1, tokens_used: 10 };
        await writeAtCaret(editor, 1, "greet them");
        expect(editor.getHTML()).toBe('<p>Hi <span data-var="">{{.FirstName}}</span>.Hello.</p>');
    });
});
