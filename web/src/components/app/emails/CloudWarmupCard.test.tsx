import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DiagnosticParticipation } from "@/lib/api/models/app/cloudlink/CloudLink";
import CloudWarmupCard from "./CloudWarmupCard";

const fixtures = vi.hoisted(() => ({ participation: undefined as DiagnosticParticipation | undefined, unavailable: false, missing: false, workspaceConnected: true, managed: false, executor: "cloud" as "cloud" | "instance", patch: vi.fn(), lifecycle: vi.fn(), unenroll: vi.fn(), confirm: vi.fn() }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ manageable: true, connected: true, unavailable: fixtures.unavailable, workspaceConnected: fixtures.workspaceConnected, isEnrolled: (id: string) => id === "mailbox", rowFor: () => ({ enrolled: true, managed: fixtures.managed, executor: fixtures.executor, cloud: fixtures.missing ? null : { participation: fixtures.participation, warmup: { enabled: true, paused: false }, settings: { base: 10 }, sent_today: 0, sent_7d: 0 } }) }) }));
vi.mock("@/lib/api/hooks/app/cloudlink/useCloudLink", () => ({ useEnrollCloudLinkMailbox: () => ({}), useUnenrollCloudLinkMailbox: () => ({ mutateAsync: fixtures.unenroll }), useCloudLinkMailboxLifecycle: () => ({ mutateAsync: fixtures.lifecycle }) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: (message: string, confirm: () => void) => { fixtures.confirm(message); confirm(); } }) }));
vi.mock("@/lib/api/client/app/cloudlink/cloudLink", () => ({ setCloudLinkParticipation: fixtures.patch }));
vi.mock("./WarmupPartnerDiversity", () => ({ default: () => null }));
vi.mock("./WarmupSendFailureNote", () => ({ default: () => null }));

describe.each([true, false])("Cloud diagnostic participation with workspace connection %s", (workspaceConnected) => {
    beforeEach(() => { fixtures.participation = undefined; fixtures.missing = fixtures.unavailable = false; fixtures.workspaceConnected = workspaceConnected; fixtures.managed = false; fixtures.executor = "cloud"; fixtures.patch.mockReset().mockResolvedValue({}); fixtures.lifecycle.mockReset().mockResolvedValue({}); fixtures.unenroll.mockReset().mockResolvedValue(undefined); fixtures.confirm.mockReset(); });
    afterEach(cleanup);
    const draw = () => render(<QueryClientProvider client={new QueryClient()}><CloudWarmupCard mailboxId="mailbox" email="fixture@example.test" provider="smtp_imap" status="active" /></QueryClientProvider>);

    it("does not infer consent or offer new controls from an old Cloud payload", () => {
        draw();
        expect(screen.getByText(/Legacy state is not proof of diagnostic consent/)).toBeInTheDocument();
        expect(screen.queryByText("Stop all diagnostic participation")).not.toBeInTheDocument();
        expect(fixtures.patch).not.toHaveBeenCalled();
    });

    it("does not describe a missing Cloud report as active diagnostics", () => {
        fixtures.missing = true;
        draw();
        expect(screen.getByText("Cloud warmup status unavailable")).toBeInTheDocument();
        expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument();
    });

    it.each([false, true])("keeps Cloud lifecycle controls based on the reported state when the local executor is off (paused: %s)", async (paused) => {
        fixtures.executor = "instance";
        if (paused) fixtures.participation = { mode: "diagnostic", send: false, receive: true };
        render(<QueryClientProvider client={new QueryClient()}><CloudWarmupCard mailboxId="mailbox" email="fixture@example.test" provider="smtp_imap" status="inactive" /></QueryClientProvider>);
        expect(screen.getByText("Mailbox off on this server")).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: paused ? "Resume" : "Pause" }));
        await waitFor(() => expect(fixtures.lifecycle).toHaveBeenCalledWith({ id: "mailbox", action: paused ? "resume" : "pause" }));
    });

    it("retains a positive mailbox report when the separate connection request fails", () => {
        fixtures.unavailable = true;
        draw();
        expect(screen.getByText("Automated diagnostics in Warmbly Cloud")).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    });

    it("shows off rather than actively warming and stops both directions with shared limits intact", async () => {
        fixtures.participation = { mode: "off", send: false, receive: false, shared_daily_limit: 8, rolling_recipient_limit: 11 };
        draw();
        expect(screen.getByText("Diagnostics off in Warmbly Cloud")).toBeInTheDocument();
        fireEvent.click(screen.getByText("Stop all diagnostic participation"));
        await waitFor(() => expect(fixtures.patch).toHaveBeenCalledWith("mailbox", fixtures.participation));
    });

    it("discloses receiving actions while keeping receive-only sending paused", () => {
        fixtures.participation = { mode: "diagnostic", send: false, receive: true };
        draw();
        expect(screen.getByText("Diagnostic sending paused in Cloud")).toBeInTheDocument();
        expect(screen.getByText(/Receive-only does not authorize replies/)).toBeInTheDocument();
        expect(screen.getByText(/Receiving allows probabilistic spam rescue, reads, importance and stars or flags where supported/)).toBeInTheDocument();
        expect(screen.getByText(/not a guaranteed reputation improvement/)).toBeInTheDocument();
    });

    it("discloses that managed removal stops Cloud warmup but preserves its mailbox", async () => {
        fixtures.managed = true;
        draw();
        fireEvent.click(screen.getByRole("button", { name: "Remove" }));
        expect(fixtures.confirm).toHaveBeenCalledWith(expect.stringContaining("stop its Cloud warmup sending and receiving"));
        expect(fixtures.confirm).toHaveBeenCalledWith(expect.stringContaining("mailbox, sign-in and history stay in your Warmbly Cloud workspace"));
        await waitFor(() => expect(fixtures.unenroll).toHaveBeenCalledWith("mailbox"));
    });
});
