import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudLinkMailboxRow, CloudLinkStatus } from "@/lib/api/models/app/cloudlink/CloudLink";
import WarmblyCloudSettingsPage from "./page";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

function draw() {
    return render(<QueryClientProvider client={new QueryClient()}><WarmblyCloudSettingsPage /></QueryClientProvider>);
}

const fixture = vi.hoisted(() => ({
    status: {} as CloudLinkStatus,
    rows: [] as CloudLinkMailboxRow[],
    enroll: vi.fn(),
    disconnect: vi.fn(),
    confirm: vi.fn(),
}));

vi.mock("@/hooks/usePermission", () => ({
    usePermission: () => true,
    useInstanceAdmin: () => ({ allowed: true }),
}));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ data: { self_hosted: true } }) }));
vi.mock("@/lib/api/hooks/app/organizations/useCurrentOrganization", () => ({ default: () => ({ data: { id: "local" } }) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: fixture.confirm }) }));
vi.mock("@/lib/api/hooks/app/cloudlink/useCloudLink", () => ({
    useCloudLinkStatus: () => ({ data: fixture.status, refetch: vi.fn() }),
    useDisconnectCloudLink: () => ({ mutateAsync: fixture.disconnect }),
    useCloudLinkMailboxes: () => ({ data: fixture.rows }),
    useEnrollCloudLinkMailbox: () => ({ mutateAsync: fixture.enroll }),
    useUnenrollCloudLinkMailbox: () => ({ mutateAsync: vi.fn() }),
    useCloudLinkMailboxLifecycle: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("./LinkedInstances", () => ({ default: () => null }));
vi.mock("@tanstack/react-router", () => ({ Navigate: ({ to }: { to: string }) => <div>Redirected to {to}</div> }));
vi.mock("@/components/app/emails/CloudMailboxPicker", () => ({ default: () => <div>Existing Cloud mailboxes</div> }));

describe("existing Cloud connections", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        fixture.enroll.mockResolvedValue(undefined);
        fixture.disconnect.mockResolvedValue(undefined);
        fixture.status = {
            connected: true,
            reachable: true,
            legacy_connected: true,
            default_cloud_url: "https://cloud.test",
            link: {
                instance_id: "original",
                cloud_url: "https://cloud.test",
                organization_name: "Existing Cloud workspace",
                connected_at: new Date("2025-01-01"),
            },
        };
        fixture.rows = [{
            id: "mailbox", email: "sender@test.local", name: "Sender",
            provider: "smtp_imap", status: "active", enrolled: false, managed: false, legacy: true,
        }];
    });

    it("shows the normal connection and allows enrollment without a migration", async () => {
        draw();
        expect(screen.getByText("Existing Cloud workspace")).toBeInTheDocument();
        expect(screen.getByText("Existing Cloud mailboxes")).toBeInTheDocument();
        expect(screen.queryByText(/legacy/i)).not.toBeInTheDocument();
        expect(screen.queryByText("Connect this workspace")).not.toBeInTheDocument();
        expect(screen.getByRole("switch")).toBeEnabled();
        fireEvent.click(screen.getByRole("switch"));
        await waitFor(() => expect(fixture.enroll).toHaveBeenCalledWith("mailbox"));
    });

    it("hands the credential of a mailbox kept on this server to Cloud only on request", async () => {
        fixture.rows[0] = { ...fixture.rows[0], enrolled: true, executor: "instance", enrollment_state: "active" };
        draw();
        expect(screen.getByText(/mailboxes whose credential stayed on this server are paused/)).toBeInTheDocument();
        expect(screen.queryByRole("group", { name: "Where the credential lives" })).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: /Hand to Cloud/ }));
        expect(fixture.enroll).not.toHaveBeenCalled();
        expect(fixture.confirm.mock.calls[0][0]).toContain("Hand sender@test.local's credential to Warmbly Cloud");
        await fixture.confirm.mock.calls[0][1]();
        expect(fixture.enroll).toHaveBeenCalledWith({ id: "mailbox", executor: "cloud" });
    });

    it("offers no handover for a Google or Microsoft mailbox kept on this server, only removal", () => {
        fixture.rows[0] = { ...fixture.rows[0], provider: "gmail", enrolled: true, executor: "instance", enrollment_state: "active" };
        draw();
        expect(screen.queryByRole("button", { name: /Hand to Cloud/ })).not.toBeInTheDocument();
        expect(screen.getByText(/remove it and add it again through Warmbly Cloud sign-in/)).toBeInTheDocument();
        expect(screen.getByRole("switch")).toBeEnabled();
    });

    it("still prevents enrollment of an unsupported local OAuth mailbox", () => {
        fixture.rows[0].provider = "gmail";
        draw();
        expect(screen.getByRole("switch")).toBeDisabled();
    });

    it("requires confirmation of server-wide disconnect scope", async () => {
        draw();
        fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
        expect(fixture.disconnect).not.toHaveBeenCalled();
        expect(fixture.confirm.mock.calls[0][0]).toContain("across all local workspaces");
        await fixture.confirm.mock.calls[0][1]();
        expect(fixture.disconnect).toHaveBeenCalledWith(true);
    });

    it("keeps both disconnect scopes explicit when connections coexist", async () => {
        fixture.status.link!.organization_id = "local";
        draw();
        expect(screen.queryByText(/legacy/i)).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
        expect(fixture.confirm.mock.calls[0][0]).toContain("this workspace");
        await fixture.confirm.mock.calls[0][1]();
        expect(fixture.disconnect).toHaveBeenLastCalledWith(false);
        fireEvent.click(screen.getByRole("button", { name: "Disconnect shared connection" }));
        expect(fixture.confirm.mock.calls[1][0]).toContain("across all local workspaces");
        await fixture.confirm.mock.calls[1][1]();
        expect(fixture.disconnect).toHaveBeenLastCalledWith(true);
    });

    it("has no page for an instance that is not linked, since Cloud links no new instances", () => {
        fixture.status.connected = false;
        fixture.status.legacy_connected = false;
        fixture.status.link = null;
        draw();
        expect(screen.getByText("Redirected to /app/settings/profile")).toBeInTheDocument();
    });
});
