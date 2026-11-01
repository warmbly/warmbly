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
vi.mock("./ConnectFlow", () => ({ default: () => <div>Connection setup</div> }));
vi.mock("./LinkedInstances", () => ({ default: () => null }));
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
        expect(screen.queryByText("Connection setup")).not.toBeInTheDocument();
        expect(screen.getByRole("switch")).toBeEnabled();
        fireEvent.click(screen.getByRole("switch"));
        await waitFor(() => expect(fixture.enroll).toHaveBeenCalledWith("mailbox"));
    });

    it("enrolls with the server executor when the Cloud grant offers it", async () => {
        fixture.status.info = { self_execution: { protocol: 1, granted: true } } as CloudLinkStatus["info"];
        draw();
        expect(screen.getByRole("group", { name: "Where the credential lives" })).toBeInTheDocument();
        fireEvent.click(screen.getByRole("switch"));
        await waitFor(() => expect(fixture.enroll).toHaveBeenCalledWith({ id: "mailbox", executor: "instance" }));
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
        expect(screen.queryByText("Connection setup")).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
        expect(fixture.confirm.mock.calls[0][0]).toContain("this workspace");
        await fixture.confirm.mock.calls[0][1]();
        expect(fixture.disconnect).toHaveBeenLastCalledWith(false);
        fireEvent.click(screen.getByRole("button", { name: "Disconnect shared connection" }));
        expect(fixture.confirm.mock.calls[1][0]).toContain("across all local workspaces");
        await fixture.confirm.mock.calls[1][1]();
        expect(fixture.disconnect).toHaveBeenLastCalledWith(true);
    });

    it("only offers connection setup when disconnected", () => {
        fixture.status.connected = false;
        fixture.status.legacy_connected = false;
        fixture.status.link = null;
        draw();
        expect(screen.getByText("Connection setup")).toBeInTheDocument();
    });
});
