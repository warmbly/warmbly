import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import WarmupSendFailureNote from "./WarmupSendFailureNote";

const failure = { message: "The sending worker has not loaded this mailbox yet.", at: "2026-10-08T08:00:00Z" };
afterEach(cleanup);

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
        expect(screen.getByText(/None has been delivered since/)).toBeInTheDocument();
    });
});
