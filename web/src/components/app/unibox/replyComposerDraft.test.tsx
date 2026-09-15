// The reply composer has to survive a thread re-render.
//
// It did not: the effect that resets the composer for a new compose session
// listed values derived from `replyTo` in its dependencies, and the thread
// rebuilds its message objects on every render. A thread re-renders constantly
// while it is open (a teammate's presence diff, an arriving mail, a mark-seen,
// any realtime invalidation), so the body was cleared between keystrokes and a
// reply could not be typed at all.
//
// What is pinned here is that a re-render carrying an equal-but-new `replyTo`
// leaves the draft alone, and that a seed still restores one.

import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

vi.mock("react-hot-toast", () => ({
    default: { success: () => {}, error: () => {} },
}));
vi.mock("@/lib/api/client/app/unibox/sendReply", () => ({ default: async () => ({}) }));
vi.mock("@/lib/api/hooks/app/templates/useTemplates", () => ({
    default: () => ({ data: { data: [] } }),
}));
vi.mock("@/lib/api/hooks/app/unibox/useUniboxOverview", () => ({
    default: () => ({ data: { scheduled_pending: 0, scheduled_pending_max: 100 } }),
}));
vi.mock("@/lib/api/hooks/app/unibox/useDraftReply", () => ({
    default: () => ({ mutateAsync: async () => ({}), isPending: false }),
}));
vi.mock("@/hooks/context/user", () => ({
    useUserProfile: () => ({ user: { id: "u1", email: "me@example.com", name: "Me" } }),
}));
vi.mock("@/stores", () => ({
    useAppStore: (sel: (s: { emails: unknown[] }) => unknown) =>
        sel({ emails: [{ id: "acc1", email: "me@example.com", signature_html: "", signature_plain: "" }] }),
}));
vi.mock("@/hooks/useOutboxStore", () => ({
    useOutboxStore: (sel: (s: { add: () => void }) => unknown) => sel({ add: () => {} }),
    resolveSendAt: () => new Date(),
}));
// The AI surfaces own a caret and a streaming callback, neither of which this
// is about; they are rendered as nothing so the textarea is the only writer.
vi.mock("@/components/app/ai/AIDraftBar", () => ({
    default: () => null,
    useAIDraft: () => ({ state: "idle", start: () => {}, keep: () => {}, discard: () => {} }),
}));
vi.mock("@/components/app/ai/TextareaAIEdit", () => ({ default: () => null }));
vi.mock("@/components/app/ai/TextareaAICaret", () => ({ default: () => null }));
vi.mock("./TemplatePicker", () => ({ default: () => null }));
vi.mock("./InsertBookingLink", () => ({ default: () => null }));
vi.mock("./compose/ContactRecipientField", () => ({ default: () => null }));
vi.mock("@/components/ui/DateTimePicker", () => ({ DateTimePicker: () => null }));

const { ReplyComposer } = await import("./ReplyComposer");

// The thread maps its payload through `toUniboxEmail` on every render, so each
// render hands the composer a new object for the same message. This builds one.
function message() {
    return {
        id: "msg-1",
        account_id: "acc1",
        from: "Them <them@example.com>",
        subject: "Quarterly numbers",
        to: [],
        cc: [],
    } as never;
}

function body() {
    return screen.getByPlaceholderText(/write/i) as HTMLTextAreaElement;
}

describe("reply composer drafts", () => {
    beforeEach(() => vi.clearAllMocks());

    it("keeps what was typed when the thread re-renders", () => {
        const { rerender } = render(
            <ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />,
        );

        fireEvent.change(body(), { target: { value: "Thanks, sending the figures over" } });
        expect(body().value).toBe("Thanks, sending the figures over");

        // A realtime invalidation: same conversation, freshly built objects.
        for (let i = 0; i < 3; i++) {
            rerender(
                <ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />,
            );
        }

        expect(body().value).toBe("Thanks, sending the figures over");
    });

    it("still restores a cancelled undo-send", () => {
        const seed = { to: ["them@example.com"], cc: [], bcc: [], subject: "Re: x", body: "restored" };
        const { rerender } = render(
            <ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />,
        );
        expect(body().value).toBe("");

        rerender(
            <ReplyComposer threadId="t1" replyTo={message()} mode="reply" seed={seed} onClose={() => {}} />,
        );
        expect(body().value).toBe("restored");
    });
});
