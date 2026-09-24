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
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act, cleanup } from "@testing-library/react";

// Exit animations never finish in jsdom; closed layers unmount at once instead.
vi.mock("framer-motion", async (importOriginal) => ({
    ...(await importOriginal<Record<string, unknown>>()),
    AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("react-hot-toast", () => ({
    default: { success: () => {}, error: () => {} },
}));
const sendReply = vi.hoisted(() => vi.fn(async () => ({})));
vi.mock("@/lib/api/client/app/unibox/sendReply", () => ({ default: sendReply }));
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
    useAppStore: (sel: (s: { emails: unknown[]; tags: unknown[]; currentOrganization: { id: string } }) => unknown) =>
        sel({
            currentOrganization: { id: "org1" },
            tags: [],
            emails: [
                { id: "acc1", email: "me@example.com", status: "active", tags: [], signature_html: "", signature_plain: "" },
                { id: "acc2", email: "other@example.com", status: "active", tags: [], signature_html: "", signature_plain: "Other signature", signature_sync: true },
                { id: "acc3", email: "gone@example.com", status: "inactive", tags: [], signature_html: "", signature_plain: "" },
            ],
        }),
}));
vi.mock("@/lib/api/hooks/app/unibox/useComposeCandidates", () => ({
    default: () => ({
        isPending: false,
        data: {
            accounts: ["me@example.com", "other@example.com"].map((email, i) => ({
                id: `acc${i + 1}`, email, name: "", provider: "gmail", auth_state: "passing", warmup_active: false,
                daily_limit: 50, sent_today: 0, remaining_today: 50, history_messages: 0, score: 1, reasons: [], recommended: i === 0,
            })),
            recommended_account_id: "acc1",
            recommended_reason: "",
            contact: null,
            suppression: null,
        },
    }),
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
vi.mock("@/lib/api/hooks/app/unibox/useUniboxEmail", () => ({
    default: () => ({
        isPending: false,
        isError: false,
        data: {
            from: ["Them <them@example.com>"],
            to: ["me@example.com"],
            cc: [],
            subject: "Quarterly numbers",
            date: new Date("2026-09-23T08:34:00Z"),
            body_html: "",
            body_plain: "The figures are attached.",
            body_truncated: true,
        },
    }),
}));
vi.mock("./EmailBody", () => ({ default: ({ plain }: { plain?: string }) => <p>{plain}</p> }));

const { ReplyComposer } = await import("./ReplyComposer");
const { replyDraftKey } = await import("@/lib/unibox/replyDraft");
const draftKey = () => replyDraftKey("u1", "org1", "t1", "msg-1", "reply");

// The thread maps its payload through `toUniboxEmail` on every render, so each
// render hands the composer a new object for the same message. This builds one.
function message(accountId = "acc1") {
    return {
        id: "msg-1",
        account_id: accountId,
        from: "Them <them@example.com>",
        subject: "Quarterly numbers",
        to: [],
        cc: [],
    } as never;
}

function body() {
    return screen.getByPlaceholderText(/write/i) as HTMLTextAreaElement;
}

function fromTrigger(email: string) {
    return screen.getAllByText(email)[0].closest("button") as HTMLButtonElement;
}

function pickSender(current: string, next: string) {
    fireEvent.click(fromTrigger(current));
    fireEvent.click(screen.getByText(next));
}

describe("reply composer drafts", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        setupStorage();
        vi.useFakeTimers();
    });
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

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
    it("restores the latest keystrokes after an immediate unmount", () => {
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "Latest keystrokes" } });
        view.unmount();
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        expect(body()).toHaveValue("Latest keystrokes");
    });

    it("saves before pagehide and shows only confirmed saves", () => {
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "Before reload" } });
        expect(screen.queryByText("Draft saved")).toBeNull();
        fireEvent(window, new Event("pagehide"));
        expect(localStorage.getItem(draftKey())).toContain("Before reload");
        fireEvent.change(body(), { target: { value: "After reload" } });
        act(() => vi.advanceTimersByTime(400));
        expect(screen.getByText("Draft saved")).toBeInTheDocument();
    });

    it("does not resurrect a discarded draft during its exit animation", () => {
        const onClose = vi.fn();
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={onClose} />);
        fireEvent.change(body(), { target: { value: "Discard me" } });
        act(() => vi.advanceTimersByTime(300));
        fireEvent.click(screen.getByRole("button", { name: "Discard" }));
        expect(onClose).toHaveBeenCalledOnce();
        act(() => vi.advanceTimersByTime(500));
        view.unmount();
        expect(localStorage.getItem(draftKey())).toBeNull();
    });

    it("does not resurrect a queued reply during its exit animation", async () => {
        const onClose = vi.fn();
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={onClose} />);
        fireEvent.change(body(), { target: { value: "Send me" } });
        act(() => vi.advanceTimersByTime(300));
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        expect(sendReply).toHaveBeenCalledOnce();
        expect(onClose).toHaveBeenCalledOnce();
        act(() => vi.advanceTimersByTime(500));
        view.unmount();
        expect(localStorage.getItem(draftKey())).toBeNull();
    });

    it("resumes persistence when undo restores the same exiting composer", async () => {
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "Original send" } });
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        const seed = { to: ["them@example.com"], cc: [], bcc: [], subject: "Re: x", body: "Original send" };
        view.rerender(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" seed={seed} onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "Edited after undo" } });
        view.unmount();
        expect(localStorage.getItem(draftKey())).toContain("Edited after undo");
    });

    it("does not delete a newer draft when an earlier send completes after navigation", async () => {
        let finish!: (value: object) => void;
        sendReply.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
        const onClose = vi.fn();
        const previous = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={onClose} />);
        fireEvent.change(body(), { target: { value: "First reply" } });
        fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true });
        previous.unmount();
        const current = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "New reply after navigation" } });
        act(() => vi.advanceTimersByTime(400));
        await act(async () => finish({}));
        expect(onClose).not.toHaveBeenCalled();
        expect(localStorage.getItem(draftKey())).toContain("New reply after navigation");
        current.unmount();
    });

    it("keeps edits made while a send is pending", async () => {
        let finish!: (value: object) => void;
        sendReply.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
        const onClose = vi.fn();
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={onClose} />);
        fireEvent.change(body(), { target: { value: "First reply" } });
        fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true });
        fireEvent.change(body(), { target: { value: "Keep editing" } });
        await act(async () => finish({}));
        expect(onClose).not.toHaveBeenCalled();
        expect(body()).toHaveValue("Keep editing");
        view.unmount();
        expect(localStorage.getItem(draftKey())).toContain("Keep editing");
    });

    it("keeps a failed send available after navigating away", async () => {
        sendReply.mockRejectedValueOnce(new Error("offline"));
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "Retry me" } });
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        view.unmount();
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        expect(body()).toHaveValue("Retry me");
    });

    it("preserves a subject-only edit and never creates an untouched draft", () => {
        let view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="forward" onClose={() => {}} />);
        act(() => vi.advanceTimersByTime(400));
        view.unmount();
        expect(localStorage.getItem(replyDraftKey("u1", "org1", "t1", "msg-1", "forward"))).toBeNull();
        view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="forward" onClose={() => {}} />);
        fireEvent.change(screen.getByPlaceholderText("Subject"), { target: { value: "Custom subject" } });
        view.unmount();
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="forward" onClose={() => {}} />);
        expect(screen.getByPlaceholderText("Subject")).toHaveValue("Custom subject");
    });

    it("forwards the message by id, with or without a note", async () => {
        const seed = { to: ["colleague@example.com"], cc: [], bcc: [], subject: "Fwd: Quarterly numbers", body: "" };
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="forward" seed={seed} onClose={() => {}} />);
        expect(screen.getByText("Forwarded message")).toBeInTheDocument();
        expect(screen.getByText(/carries that preview/)).toBeInTheDocument();

        const send = screen.getByRole("button", { name: "Send" });
        expect(send).toBeEnabled();
        await act(async () => fireEvent.click(send));
        expect(sendReply).toHaveBeenCalledWith(expect.objectContaining({
            to: ["colleague@example.com"],
            forward_message_id: "msg-1",
            thread_id: undefined,
            body_plain: "",
        }));
    });

    it("previews the forwarded message on demand", () => {
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="forward" onClose={() => {}} />);
        expect(screen.queryByText("The figures are attached.")).toBeNull();
        fireEvent.click(screen.getByRole("button", { name: /Forwarded message/ }));
        expect(screen.getByText("The figures are attached.")).toBeInTheDocument();
        expect(screen.getByText("Attachments on the original are not forwarded.")).toBeInTheDocument();
    });

    it("keeps a reply's body required and never names a message to forward", async () => {
        const seed = { to: ["them@example.com"], cc: [], bcc: [], subject: "Re: x", body: "" };
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" seed={seed} onClose={() => {}} />);
        expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
        expect(screen.queryByText("Forwarded message")).toBeNull();
        fireEvent.change(body(), { target: { value: "Thanks" } });
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        expect(sendReply).toHaveBeenCalledWith(expect.objectContaining({ thread_id: "t1", forward_message_id: undefined }));
        view.unmount();
    });

    it("sends from the mailbox picked in From and keeps the conversation", async () => {
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        expect(screen.queryByText(/Replying from another mailbox/)).toBeNull();
        pickSender("me@example.com", "other@example.com");
        expect(screen.getByText(/Replying from another mailbox/)).toBeInTheDocument();
        expect(screen.getByText("Other signature")).toBeInTheDocument();
        fireEvent.change(body(), { target: { value: "From the other address" } });
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        expect(sendReply).toHaveBeenCalledWith(expect.objectContaining({ email_account_id: "acc2", thread_id: "t1" }));
    });

    it("keeps the picked mailbox in the draft and in a restored undo-send", () => {
        const view = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        pickSender("me@example.com", "other@example.com");
        view.unmount();
        expect(JSON.parse(localStorage.getItem(draftKey()) ?? "{}")).toMatchObject({ email_account_id: "acc2" });
        const reopened = render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        expect(fromTrigger("other@example.com")).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Switch back" }));
        expect(fromTrigger("me@example.com")).toBeInTheDocument();
        const seed = { to: ["them@example.com"], cc: [], bcc: [], subject: "Re: x", body: "restored", email_account_id: "acc2" };
        reopened.rerender(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" seed={seed} onClose={() => {}} />);
        expect(fromTrigger("other@example.com")).toBeInTheDocument();
    });

    it("will not send from an inactive mailbox and says how to fix it", async () => {
        render(<ReplyComposer threadId="t1" replyTo={message("acc3")} mode="reply" onClose={() => {}} />);
        fireEvent.change(body(), { target: { value: "Still here?" } });
        expect(screen.getByRole("status")).toHaveTextContent("gone@example.com is not active");
        expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        expect(sendReply).not.toHaveBeenCalled();
        pickSender("gone@example.com", "other@example.com");
        expect(screen.queryByRole("status")).toBeNull();
        await act(async () => fireEvent.keyDown(body(), { key: "Enter", ctrlKey: true }));
        expect(sendReply).toHaveBeenCalledWith(expect.objectContaining({ email_account_id: "acc2" }));
    });

    it("falls back to the thread's mailbox when a saved one is gone", () => {
        localStorage.setItem(draftKey(), JSON.stringify({ to: ["them@example.com"], cc: [], bcc: [], subject: "Re: x", body: "saved", email_account_id: "acc9" }));
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={() => {}} />);
        expect(body()).toHaveValue("saved");
        expect(fromTrigger("me@example.com")).toBeInTheDocument();
        expect(screen.queryByRole("status")).toBeNull();
    });

    it("closes only the mailbox menu on Escape", () => {
        const onClose = vi.fn();
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="forward" onClose={onClose} />);
        fireEvent.click(fromTrigger("me@example.com"));
        const search = screen.getByPlaceholderText("Search mailboxes…");
        expect(screen.queryByText("Auto")).toBeNull();
        fireEvent.keyDown(search, { key: "Escape" });
        expect(screen.queryByPlaceholderText("Search mailboxes…")).toBeNull();
        expect(fromTrigger("me@example.com")).toHaveFocus();
        expect(onClose).not.toHaveBeenCalled();
    });

    it("does not claim a failed save succeeded or close away the unsaved text", () => {
        vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw new Error("quota"); });
        const onClose = vi.fn();
        render(<ReplyComposer threadId="t1" replyTo={message()} mode="reply" onClose={onClose} />);
        fireEvent.change(body(), { target: { value: "Keep me" } });
        act(() => vi.advanceTimersByTime(400));
        expect(screen.queryByText("Draft saved")).toBeNull();
        expect(screen.getByText("Draft not saved")).toBeInTheDocument();
        fireEvent.click(screen.getByLabelText("Close composer, keeping the draft"));
        expect(onClose).not.toHaveBeenCalled();
        expect(body()).toHaveValue("Keep me");
    });

});


function setupStorage() {
    const values = new Map<string, string>();
    vi.mocked(localStorage.getItem).mockImplementation((key: string) => values.get(key) ?? null);
    vi.mocked(localStorage.setItem).mockImplementation((key: string, value: string) => { values.set(key, value); });
    vi.mocked(localStorage.removeItem).mockImplementation((key: string) => { values.delete(key); });
}
