import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { createPassthroughRouter, renderRouter } from "@/test/routerHarness";
import { currentOrganizationQuery } from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { storeStatusQuery } from "@/lib/api/hooks/app/store/useStore";
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";
import type { StoreStatus } from "@/lib/api/models/app/store/Store";
import { useAppStore } from "@/stores";
import BuyMailboxesPage from "./page";

vi.mock("@/lib/api/client/Request", () => ({ default: async () => ({ data: [], pagination: { has_more: false } }) }));

const locked: StoreStatus = {
    enabled: false,
    metered: false,
    currency: "usd",
    mailbox_monthly_micros: 3_000_000,
    domain_markup: "at_cost",
    limits: { mailboxes_per_domain_max: 5, mailboxes_per_domain_recommended: 3 },
    providers: ["google", "microsoft"],
    balance: null,
    registrant: null,
    plan_active: true,
    configured: false,
    reason: "not_configured",
    locked: true,
    can_configure: false,
};

async function draw(status: StoreStatus, partner?: PartnerManaged) {
    useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(currentOrganizationQuery.queryKey, { id: "workspace", role: "owner", partner } as never);
    client.setQueryData(storeStatusQuery.queryKey, status);
    await renderRouter(createPassthroughRouter(<BuyMailboxesPage />, "/app/emails/buy"), client);
}

afterEach(cleanup);

describe("the buy page without GraphKit", () => {
    it("shows an instance admin how to connect it, with no Orders link", async () => {
        await draw({ ...locked, can_configure: true, configure_url: "https://admin.example.test/setup" });
        expect(await screen.findByText("This instance isn't set up for it yet. To buy domains and mailboxes, add a GraphKit API key.")).toBeInTheDocument();
        expect(screen.getByRole("link", { name: /Add GraphKit API key/ })).toHaveAttribute("href", "https://admin.example.test/setup");
        expect(screen.getByRole("list", { name: "Setup steps" })).toBeInTheDocument();
        expect(screen.queryByRole("link", { name: "Orders" })).not.toBeInTheDocument();
    });

    it("asks a member to reach their operator", async () => {
        await draw(locked);
        expect(await screen.findByText("Ask your instance operator to add a GraphKit API key")).toBeInTheDocument();
        expect(screen.getByRole("button", { name: /Copy message/ })).toBeInTheDocument();
        expect(screen.queryByRole("link", { name: /Add GraphKit API key/ })).not.toBeInTheDocument();
    });

    it("keeps Orders and the paused notice for a workspace with live purchases", async () => {
        await draw({ ...locked, disconnected: { mailboxes: 2, domains: 1 } });
        expect(await screen.findByRole("status")).toHaveTextContent(/Renewals, monthly billing and Google mailbox connections are paused/);
        expect(screen.getByRole("link", { name: "Orders" })).toBeInTheDocument();
    });

    it("stays unavailable, not locked, on a hosted instance not configured yet", async () => {
        await draw({ ...locked, locked: false, disconnected: { mailboxes: 2, domains: 1 } });
        expect(await screen.findByText("Buying mailboxes is not available here")).toBeInTheDocument();
        expect(screen.queryByText(/GraphKit/)).not.toBeInTheDocument();
        expect(screen.queryByText(/Ask your instance operator/)).not.toBeInTheDocument();
        expect(screen.queryByRole("status")).not.toBeInTheDocument();
    });

    it("stays unavailable, not locked, in a workspace a reseller manages", async () => {
        await draw(locked, { display_name: "Acme Reseller", state: "active" });
        expect(await screen.findByText("Buying mailboxes is not available here")).toBeInTheDocument();
        expect(screen.queryByText(/isn't set up on this instance/)).not.toBeInTheDocument();
    });
});
