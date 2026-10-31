import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import WarmblyCloudSettingsPage from "./page";

const fixture = vi.hoisted(() => ({ paused: false, statusReads: 0, mailboxReads: 0 }));
vi.mock("@/hooks/usePermission", () => ({ usePermission: () => true, useInstanceAdmin: () => ({ allowed: true }) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: vi.fn() }) }));
vi.mock("@/components/app/emails/CloudMailboxPicker", () => ({ default: () => null }));
vi.mock("@/lib/api/client/Request", () => ({ default: async ({ url }: { url: string }) => {
    if (url === "/auth/config") return { self_hosted: true };
    if (url === "/cloud-link") {
        fixture.statusReads++;
        return { connected: true, reachable: true, link: { organization_id: "workspace", organization_name: "Cloud workspace", cloud_url: "https://cloud.test" } };
    }
    if (url === "/cloud-link/mailboxes") {
        fixture.mailboxReads++;
        return { data: [{ id: "mailbox", email: "sender@example.test", provider: "smtp_imap", status: "active", enrolled: true,
            cloud: { status: "active", settings: { base: 10 }, sent_today: 5, warmup: { enabled: true, paused: fixture.paused }, health: { state: "healthy" } },
        }] };
    }
    throw new Error(`Unexpected request ${url}`);
} }));
afterEach(cleanup);

it("Refresh fetches both the connection and mailbox reports without a socket", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    render(<QueryClientProvider client={client}><WarmblyCloudSettingsPage /></QueryClientProvider>);
    await screen.findByText("sender@example.test");
    const before = { status: fixture.statusReads, mailboxes: fixture.mailboxReads };
    fixture.paused = true;
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(await screen.findByText(/warmup paused/)).toBeInTheDocument();
    expect(fixture.statusReads).toBeGreaterThan(before.status);
    expect(fixture.mailboxReads).toBeGreaterThan(before.mailboxes);
});
