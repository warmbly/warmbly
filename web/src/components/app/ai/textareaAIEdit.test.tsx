// The unibox composer caps its body length, so a rewrite near that cap is
// written truncated. The range recorded for Undo and Again has to describe what
// the box actually holds, or both act on text that is not there (issue #432
// review).

import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

let reply = { text: "", credits_charged: 1, tokens_used: 10 };
let sent: { text: string; instruction: string } | null = null;
vi.mock("@/lib/api/hooks/app/generation/useGenerateEdit", () => ({
    default: () => ({
        isPending: false,
        mutate: (
            vars: { text: string; instruction: string },
            opts?: { onSuccess?: (res: unknown) => void },
        ) => {
            sent = vars;
            opts?.onSuccess?.(reply);
        },
    }),
}));
// The typewriter animates the rewrite in; the test wants its end state.
vi.mock("./useTypewriter", () => ({
    default: () => ({
        run: (text: string, onTick: (partial: string) => void, onDone: () => void) => {
            onTick(text);
            onDone();
        },
        cancel: () => {},
    }),
}));

// jsdom has no layout, so the selection measurement that anchors the pill
// returns nothing real; the flow under test is what happens after it.
vi.mock("./textareaRange", () => ({
    default: () => ({ top: 300, left: 10, bottom: 320, centerX: 100 }),
    textareaRangeRects: () => [],
}));

import TextareaAIEdit from "./TextareaAIEdit";

const MAX = 20;

function Host({ initial }: { initial: string }) {
    const ref = React.useRef<HTMLTextAreaElement>(null);
    const [value, setValue] = React.useState(initial);
    return (
        <>
            <textarea ref={ref} value={value} onChange={(e) => setValue(e.target.value)} />
            <TextareaAIEdit textareaRef={ref} value={value} onChange={setValue} maxLen={MAX} />
        </>
    );
}

describe("TextareaAIEdit against the composer's length cap", () => {
    beforeEach(() => {
        reply = { text: "", credits_charged: 1, tokens_used: 10 };
        sent = null;
    });

    it("records the range that is really in the box after the cap cut the tail", async () => {
        const initial = "one two three";
        const { container } = render(
            <QueryClientProvider client={new QueryClient()}>
                <Host initial={initial} />
            </QueryClientProvider>,
        );
        const ta = container.querySelector("textarea")!;

        // Select "two" and ask for a rewrite long enough to overrun the cap.
        await act(async () => {
            ta.focus();
            ta.setSelectionRange(4, 7);
            document.dispatchEvent(new Event("selectionchange"));
        });
        reply = { text: "TWO IS MUCH LONGER", credits_charged: 1, tokens_used: 10 };
        await act(async () => {
            fireEvent.mouseDown(await screen.findByText("Edit with AI"));
        });
        const input = await screen.findByPlaceholderText("Tell AI how to change it…");
        await act(async () => {
            fireEvent.change(input, { target: { value: "expand" } });
            fireEvent.keyDown(input, { key: "Enter" });
        });

        // The cap truncated the written value…
        expect(ta.value).toBe("one TWO IS MUCH LONG");
        expect(ta.value.length).toBe(MAX);

        // …and Again still re-sends the words that were selected, rather than a
        // range worked back out of the lengths the truncation invalidated.
        await act(async () => {
            fireEvent.click(screen.getByText("Again"));
        });
        expect(sent?.text).toBe("two");
    });

    it("undoes back to exactly the body that was there", async () => {
        const initial = "one two three";
        const { container } = render(
            <QueryClientProvider client={new QueryClient()}>
                <Host initial={initial} />
            </QueryClientProvider>,
        );
        const ta = container.querySelector("textarea")!;
        await act(async () => {
            ta.focus();
            ta.setSelectionRange(4, 7);
            document.dispatchEvent(new Event("selectionchange"));
        });
        reply = { text: "TWO IS MUCH LONGER", credits_charged: 1, tokens_used: 10 };
        await act(async () => {
            fireEvent.mouseDown(await screen.findByText("Edit with AI"));
        });
        const input = await screen.findByPlaceholderText("Tell AI how to change it…");
        await act(async () => {
            fireEvent.change(input, { target: { value: "expand" } });
            fireEvent.keyDown(input, { key: "Enter" });
        });
        await act(async () => {
            fireEvent.click(screen.getByText("Undo"));
        });
        expect(ta.value).toBe(initial);
    });
});
