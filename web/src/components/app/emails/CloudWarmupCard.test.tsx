import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DiagnosticParticipation } from "@/lib/api/models/app/cloudlink/CloudLink";
import CloudWarmupCard from "./CloudWarmupCard";

const fixtures = vi.hoisted(() => ({ participation: undefined as DiagnosticParticipation | undefined, workspaceConnected: true, patch: vi.fn() }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ manageable: true, connected: true, workspaceConnected: fixtures.workspaceConnected, isEnrolled: (id: string) => id === "mailbox", rowFor: () => ({ enrolled: true, managed: false, cloud: { participation: fixtures.participation, settings: { base: 10 }, sent_today: 0, sent_7d: 0 } }) }) }));
vi.mock("@/lib/api/hooks/app/cloudlink/useCloudLink", () => ({ useEnrollCloudLinkMailbox: () => ({}), useUnenrollCloudLinkMailbox: () => ({}), useCloudLinkMailboxLifecycle: () => ({}) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: (_: string, confirm: () => void) => confirm() }) }));
vi.mock("@/lib/api/client/app/cloudlink/cloudLink", () => ({ setCloudLinkParticipation: fixtures.patch }));
vi.mock("./WarmupPartnerDiversity", () => ({ default: () => null }));
vi.mock("./WarmupSendFailureNote", () => ({ default: () => null }));

describe.each([true, false])("Cloud diagnostic participation with workspace connection %s", (workspaceConnected) => {
    beforeEach(() => { fixtures.participation = undefined; fixtures.workspaceConnected = workspaceConnected; fixtures.patch.mockReset().mockResolvedValue({}); });
    afterEach(cleanup);
    const draw = () => render(<QueryClientProvider client={new QueryClient()}><CloudWarmupCard mailboxId="mailbox" email="fixture@example.test" provider="smtp_imap" /></QueryClientProvider>);

    it("does not infer consent or offer new controls from an old Cloud payload", () => {
        draw();
        expect(screen.getByText(/Legacy state is not proof of diagnostic consent/)).toBeInTheDocument();
        expect(screen.queryByText("Stop all diagnostic participation")).not.toBeInTheDocument();
        expect(fixtures.patch).not.toHaveBeenCalled();
    });

    it("shows off rather than actively warming and stops both directions with shared limits intact", async () => {
        fixtures.participation = { mode: "off", send: false, receive: false, shared_daily_limit: 8, rolling_recipient_limit: 11 };
        draw();
        expect(screen.getByText("Diagnostics off in Warmbly Cloud")).toBeInTheDocument();
        fireEvent.click(screen.getByText("Stop all diagnostic participation"));
        await waitFor(() => expect(fixtures.patch).toHaveBeenCalledWith("mailbox", fixtures.participation));
    });

    it("labels receive-only as sending paused, without claiming replies are authorized", () => {
        fixtures.participation = { mode: "diagnostic", send: false, receive: true };
        draw();
        expect(screen.getByText("Diagnostic sending paused in Cloud")).toBeInTheDocument();
        expect(screen.getByText(/Receive-only does not authorize replies/)).toBeInTheDocument();
    });
});
