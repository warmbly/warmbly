import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createPassthroughRouter, renderRouter } from "@/test/routerHarness";
import { QueryClient } from "@tanstack/react-query";
import { currentOrganizationQuery } from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { storeListable, storeState, storeStatusQuery } from "@/lib/api/hooks/app/store/useStore";
import type { StoreStatus } from "@/lib/api/models/app/store/Store";
import { useAppStore } from "@/stores";
import { addMailboxesCopy } from "./addMailboxesCopy";
import { AddAccountMenu, BuyMailboxesButton } from "./BuyEntry";
import StoreLocked, { STORE_SETUP_DOCS } from "./StoreLocked";

const base: StoreStatus = {
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

// A hosted instance without a key: the same answer, but not locked.
const hosted: StoreStatus = { ...base, locked: false };

const fixture = vi.hoisted(() => ({ status: {} as Record<string, unknown>, partner: null as unknown }));

vi.mock("@/lib/api/client/Request", () => ({
    default: async ({ url }: { url: string }) => {
        if (url === "/organization/current") return { id: "workspace", role: "owner", partner: fixture.partner ?? undefined };
        if (url === "/store/status") return fixture.status;
        return { data: [], pagination: { has_more: false } };
    },
}));

beforeEach(() => {
    fixture.status = { ...base };
    fixture.partner = null;
    useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
});
afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
});

describe("storeState", () => {
    it("locks only a store waiting on GraphKit, and never in a managed workspace", () => {
        expect(storeState(base, false).kind).toBe("locked");
        expect(storeState(base, true).kind).toBe("hidden");
        expect(storeState({ ...base, reason: "partner_managed" }, false).kind).toBe("hidden");
        // A hosted instance stays hidden until its operators add the key, purchases or not.
        expect(storeState(hosted, false).kind).toBe("hidden");
        expect(storeListable(storeState({ ...hosted, disconnected: { mailboxes: 2, domains: 1 } }, false))).toBe(false);
        // An older backend sends no locked flag or no reason: the store stays hidden as before.
        expect(storeState({ ...base, locked: undefined }, false).kind).toBe("hidden");
        expect(storeState({ ...base, reason: undefined }, false).kind).toBe("hidden");
        expect(storeState({ ...base, enabled: true, reason: undefined }, false).kind).toBe("on");
    });

    it("keeps the empty Mailboxes page connect-first while locked", () => {
        const storeOn = storeState(base, false).kind === "on";
        expect(addMailboxesCopy({ partner: null, storeOn, writable: true }).body).not.toMatch(/buy/i);
    });
});

// Both reads are seeded, so the first render is already the settled one.
async function drawMenu() {
    const onConnect = vi.fn();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(currentOrganizationQuery.queryKey, { id: "workspace", role: "owner", partner: fixture.partner ?? undefined } as never);
    client.setQueryData(storeStatusQuery.queryKey, fixture.status as unknown as StoreStatus);
    await renderRouter(createPassthroughRouter(<><AddAccountMenu onConnect={onConnect} /><BuyMailboxesButton /></>), client);
    return onConnect;
}

describe("AddAccountMenu", () => {
    it("offers buying as a feature still to be set up, without Orders", async () => {
        await drawMenu();
        fireEvent.click(screen.getByRole("button", { name: /Add account/ }));
        const buy = await screen.findByRole("menuitem", { name: /Buy new mailboxes/ });
        expect(within(buy).getByText("Set up")).toBeInTheDocument();
        expect(within(buy).getByText("Not set up on this instance yet")).toBeInTheDocument();
        expect(screen.queryByRole("menuitem", { name: /Orders/ })).not.toBeInTheDocument();
        expect(screen.getByRole("menuitem", { name: /Connect a mailbox you have/ })).toBeInTheDocument();
        // The empty state's lead stays away while the store is locked.
        expect(screen.queryByRole("link", { name: /Buy new mailboxes/ })).not.toBeInTheDocument();
    });

    it("stays the plain connect button on a hosted instance not configured yet", async () => {
        fixture.status = { ...hosted };
        const onConnect = await drawMenu();
        const button = screen.getByRole("button", { name: /Add account/ });
        expect(button).not.toHaveAttribute("aria-expanded");
        fireEvent.click(button);
        expect(onConnect).toHaveBeenCalled();
        expect(screen.queryByRole("menuitem", { name: /Buy new mailboxes/ })).not.toBeInTheDocument();
        expect(screen.queryByRole("link", { name: /Buy new mailboxes/ })).not.toBeInTheDocument();
    });

    it("stays the plain connect button in a workspace a reseller manages", async () => {
        fixture.partner = { display_name: "Acme Reseller", state: "active" };
        // Even a not_configured answer stays hidden there: only the reseller sells to it.
        const onConnect = await drawMenu();
        const button = screen.getByRole("button", { name: /Add account/ });
        expect(button).not.toHaveAttribute("aria-expanded");
        fireEvent.click(button);
        expect(onConnect).toHaveBeenCalled();
        expect(screen.queryByRole("menuitem", { name: /Buy new mailboxes/ })).not.toBeInTheDocument();
    });
});

describe("StoreLocked", () => {
    it("walks an instance admin through connecting GraphKit", () => {
        render(<StoreLocked status={{ ...base, can_configure: true, configure_url: "https://admin.example.test/setup#integrations" }} />);
        expect(screen.getByText("This instance isn't set up for it yet. To buy domains and mailboxes, add a GraphKit API key.")).toBeInTheDocument();
        expect(screen.getByRole("link", { name: /Add GraphKit API key/ })).toHaveAttribute("href", "https://admin.example.test/setup#integrations");
        expect(within(screen.getByRole("list", { name: "Setup steps" })).getAllByRole("listitem")).toHaveLength(3);
        expect(screen.queryByText(/Ask your instance operator/)).not.toBeInTheDocument();
    });

    it("shows the steps without a button when the admin panel's address is unknown", () => {
        render(<StoreLocked status={{ ...base, can_configure: true }} />);
        expect(screen.queryByRole("link", { name: /Add GraphKit API key/ })).not.toBeInTheDocument();
        expect(screen.getByText(/admin panel's Setup and health page/)).toBeInTheDocument();
        expect(within(screen.getByRole("list", { name: "Setup steps" })).getAllByRole("listitem")).toHaveLength(3);
    });

    it("gives a member a note for their operator", async () => {
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
        render(<StoreLocked status={base} />);
        expect(screen.getByText("Ask your instance operator to add a GraphKit API key")).toBeInTheDocument();
        expect(screen.queryByRole("list", { name: "Setup steps" })).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: /Copy message/ }));
        await waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining(STORE_SETUP_DOCS)));
    });

    it("puts the paused notice first while the workspace holds live purchases", () => {
        render(<StoreLocked status={{ ...base, disconnected: { mailboxes: 3, domains: 1 } }} />);
        expect(screen.getByRole("status")).toHaveTextContent("paused until your instance operator adds a GraphKit API key");
    });
});
