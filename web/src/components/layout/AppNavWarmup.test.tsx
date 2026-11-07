import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { UpgradeDialogContext } from "@/hooks/context/upgrade";
import { PERMISSION_BITS } from "@/lib/permissions";
import { useAppStore } from "@/stores";
import { AppNav } from "./AppNav";

const fixture = vi.hoisted(() => ({
    emails: Array.from({ length: 8 }, (_, i) => ({ id: `mailbox-${i}`, email: `sender-${i}@example.test`, status: "active", tags: [], campaign_limit: 50, warmup: i < 6 ? "2026-10-01T00:00:00Z" : null, warmup_paused_at: null })),
    states: [] as string[],
    loading: false,
    reachable: true,
}));

vi.mock("@/lib/api/client/Request", () => ({ default: async ({ url }: { url: string }) => {
    if (url === "/auth/config") return { self_hosted: true, billing_enabled: false };
    if (url === "/auth/me") return { id: "user", admin_permissions: 0 };
    if (url === "/organization/current") return { id: "workspace", role: "member", permissions: PERMISSION_BITS.MANAGE_EMAILS };
    if (url.split("?")[0] === "/emails") return { data: fixture.emails, pagination: { has_more: false } };
    if (url === "/cloud-link") return { connected: true, reachable: fixture.reachable };
    if (url === "/cloud-link/mailboxes") {
        if (fixture.loading) return new Promise(() => {});
        return { data: fixture.emails.map((email, i) => {
            const state = fixture.states[i] ?? "active";
            return { ...email, enrolled: true, enrollment_state: state === "pending" ? "pending_enroll" : "active", cloud: state === "missing" ? null : {
                status: "active", warmup: { enabled: state !== "off", paused: state === "paused" },
                participation: state === "receive" ? { mode: "diagnostic", send: false, receive: true } : undefined,
            } };
        }) };
    }
    return { data: [], pagination: { has_more: false }, daily_trend: [] };
} }));

beforeEach(() => {
    fixture.states = []; fixture.loading = false; fixture.reachable = true;
    vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    useAppStore.setState({ user: null, navCollapsed: false, currentOrganization: { id: "workspace", name: "Workspace", role: "member", permissions: PERMISSION_BITS.MANAGE_EMAILS } });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function draw() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree: createRootRoute({ component: AppNav }), history: createMemoryHistory({ initialEntries: ["/"] }) });
    await router.load();
    render(<QueryClientProvider client={client}><UpgradeDialogContext.Provider value={{ open: vi.fn(), close: vi.fn() }}><RouterProvider router={router} /></UpgradeDialogContext.Provider></QueryClientProvider>);
    await screen.findByTitle(/^8 mailboxes/);
}

it("counts all eight Cloud warmups even when two mailboxes never started local warmup", async () => {
    await draw();
    await waitFor(() => expect(screen.getByTitle(/^8 mailboxes/)).toHaveAttribute("title", "8 mailboxes, 8 warming up"));
});

it("excludes paused, off and receive-only Cloud mailboxes from the active count", async () => {
    fixture.states = ["paused", "off", "receive"];
    await draw();
    await waitFor(() => expect(screen.getByTitle(/^8 mailboxes/)).toHaveAttribute("title", "8 mailboxes, 5 warming up"));
});

it.each(["loading", "missing", "pending"])("does not fall back to the six local anchors while a Cloud report is %s", async (state) => {
    fixture.loading = state === "loading";
    fixture.states = Array(8).fill(state);
    await draw();
    await waitFor(() => expect(screen.getByTitle(/^8 mailboxes/)).toHaveTextContent("—"));
    expect(screen.queryByTitle("8 mailboxes, 6 warming up")).not.toBeInTheDocument();
});

it("keeps positive mailbox reports independent of the connection reachability flag", async () => {
    fixture.reachable = false;
    await draw();
    await waitFor(() => expect(screen.getByTitle(/^8 mailboxes/)).toHaveAttribute("title", "8 mailboxes, 8 warming up"));
});
