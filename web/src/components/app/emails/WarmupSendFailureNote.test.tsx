import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import WarmupSendFailureNote from "./WarmupSendFailureNote";

const failure = { message: "A temporary provider error occurred.", at: "2026-10-08T08:00:00Z" };
afterEach(() => { cleanup(); vi.useRealTimers(); });
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-09T05:35:00Z")); });

describe("WarmupSendFailureNote", () => {
    it.each(["gmail", "outlook"])("does not blame SMTP settings for a %s sign-in", (provider) => {
        render(<WarmupSendFailureNote failure={failure} provider={provider} cloud />);
        expect(screen.getByText(failure.message)).toBeInTheDocument();
        expect(screen.getByText(/provider's API, not SMTP/)).toBeInTheDocument();
        expect(screen.queryByText(/SMTP host|IP allowlist|password/)).not.toBeInTheDocument();
        expect(screen.getByText(/reconnect only if the provider reports/)).toBeInTheDocument();
    });

    it("makes Cloud SMTP network advice conditional on the reported error", () => {
        render(<WarmupSendFailureNote failure={failure} provider="smtp_imap" cloud />);
        expect(screen.getByText(/If the error above reports a mail-server connection problem/)).toHaveTextContent(/IP allowlist/);
        expect(screen.getByText(/Warmbly Cloud connects from its own network/)).toBeInTheDocument();
    });

    it("keeps credential guidance conditional for local SMTP mailboxes", () => {
        render(<WarmupSendFailureNote failure={failure} provider="smtp_imap" />);
        expect(screen.getByText(/If the error above reports a mail-server connection problem/)).toHaveTextContent(/password/);
        expect(screen.queryByText(/Cloud connects/)).not.toBeInTheDocument();
    });

    it("does not prescribe a connection fix when the provider is unknown", () => {
        render(<WarmupSendFailureNote failure={failure} />);
        expect(screen.getByText(/worker-loading failure alone does not mean the credentials are wrong/)).toBeInTheDocument();
        expect(screen.queryByText(/SMTP host|IP allowlist/)).not.toBeInTheDocument();
    });

    it("preserves the actual provider failure rather than hiding it", () => {
        render(<WarmupSendFailureNote failure={{ ...failure, message: "Microsoft rejected the expired sign-in." }} provider="outlook" cloud />);
        expect(screen.getByText("Microsoft rejected the expired sign-in.")).toBeInTheDocument();
        expect(screen.getByText(/No later successful warmup send has been confirmed/)).toBeInTheDocument();
    });

    const loading = { message: "The sending worker has not loaded this mailbox yet.", at: "2026-10-09T05:30:00Z", kind: "mailbox_loading" as const, first_failure_at: "2026-10-09T03:00:00Z" };

    it("shows only a fresh persistent loading incident without blaming credentials", () => {
        vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-09T05:35:00Z"));
        render(<WarmupSendFailureNote failure={loading} provider="smtp_imap" cloud />);
        expect(screen.getByText(/repeatedly been unable to load/)).toBeInTheDocument();
        expect(screen.getByText(/does not mean your credentials are wrong/)).toBeInTheDocument();
        expect(screen.queryByText(/password|IP allowlist/)).not.toBeInTheDocument();
    });

    it.each([
        { ...loading, first_failure_at: "2026-10-09T05:30:00Z" },
        { ...loading, first_failure_at: undefined },
        { ...loading, at: "2026-10-09T04:35:00Z" },
        { ...loading, at: "2026-10-09T06:00:00Z" },
        { ...loading, at: "invalid" },
    ])("hides transient, stale, legacy or invalid loading evidence: %j", (value) => {
        vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-09T05:35:00Z"));
        const { container } = render(<WarmupSendFailureNote failure={value} />);
        expect(container).toBeEmptyDOMElement();
    });

    it("expires a loading warning even if the open page never receives another send", () => {
        vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-09T05:35:00Z"));
        const { container, rerender } = render(<WarmupSendFailureNote failure={loading} />);
        act(() => vi.advanceTimersByTime(55 * 60 * 1000));
        expect(container).toBeEmptyDOMElement();
        rerender(<WarmupSendFailureNote failure={{ ...loading, at: "2026-10-09T06:25:00Z" }} />);
        expect(screen.getByText(/repeatedly been unable to load/)).toBeInTheDocument();
    });

    it("expires an old provider failure without waiting for a new send", () => {
        const { container } = render(<WarmupSendFailureNote failure={failure} />);
        act(() => vi.advanceTimersByTime(3 * 60 * 60 * 1000));
        expect(container).toBeEmptyDOMElement();
    });
});
