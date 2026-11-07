// The AI draft card edits a pending draft in place. An API key can replace that
// draft's body (PUT /unibox/ai-drafts/by-thread), so a refetch brings the same
// draft id with new text: the card follows it, unless a person has typed in the
// box, whose edit must never be wiped.

import React from "react";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const drafts = vi.hoisted(() => ({ data: [] as unknown[] }));

vi.mock("@/lib/api/hooks/app/unibox/useAgentDrafts", () => ({
    default: () => ({ data: { data: drafts.data } }),
}));
vi.mock("@/lib/api/client/app/unibox/agentDrafts", () => ({
    approveAgentDraft: vi.fn(),
    discardAgentDraft: vi.fn(),
}));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => vi.fn() }));
vi.mock("react-hot-toast/headless", () => ({ default: { success: vi.fn(), error: vi.fn() } }));

import AgentDraftCard from "./AgentDraftCard";

const draft = (id: string, body: string) => ({
    id,
    thread_id: "th-1",
    status: "pending",
    to_addr: "lead@example.test",
    intent_class: "",
    body,
});

function view() {
    const client = new QueryClient();
    return (
        <QueryClientProvider client={client}>
            <AgentDraftCard threadId="th-1" />
        </QueryClientProvider>
    );
}

const textarea = () => screen.getByRole("textbox") as HTMLTextAreaElement;

afterEach(() => {
    cleanup();
    drafts.data = [];
});

describe("AgentDraftCard", () => {
    it("follows a replaced body while the box is untouched", () => {
        drafts.data = [draft("d1", "first")];
        const { rerender } = render(view());
        expect(textarea().value).toBe("first");

        drafts.data = [draft("d1", "second")];
        rerender(view());
        expect(textarea().value).toBe("second");
    });

    it("keeps a half-typed edit when the same draft is replaced", () => {
        drafts.data = [draft("d1", "first")];
        const { rerender } = render(view());
        fireEvent.change(textarea(), { target: { value: "my edit" } });

        drafts.data = [draft("d1", "second")];
        rerender(view());
        expect(textarea().value).toBe("my edit");
    });

    it("seeds a different draft even after an edit", () => {
        drafts.data = [draft("d1", "first")];
        const { rerender } = render(view());
        fireEvent.change(textarea(), { target: { value: "my edit" } });

        drafts.data = [draft("d2", "other")];
        rerender(view());
        expect(textarea().value).toBe("other");
    });
});
