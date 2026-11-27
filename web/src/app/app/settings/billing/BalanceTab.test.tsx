import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { parseSearch, stringifySearch } from "@/lib/routerSearch";
import type { AutoReload, Balance } from "@/lib/api/models/app/subscription/Credits";

const api = vi.hoisted(() => ({
    getBalance: vi.fn(),
    getBalanceWithBurn: vi.fn(),
    updateAutoReload: vi.fn(),
    topUpBalance: vi.fn(),
    createBalanceCheckout: vi.fn(),
    createPaymentMethodSetup: vi.fn(),
    createCardSetupIntent: vi.fn(),
    adoptCardSetup: vi.fn(),
    listBalanceInvoices: vi.fn(),
}));
vi.mock("@/lib/api/client/app/subscription/balance", () => api);

const store = vi.hoisted(() => ({ enabled: false, minCardChargeMicros: 10_000_000 }));
vi.mock("@/lib/api/hooks/app/store/useStore", () => ({
    useStoreEnabled: () => ({
        enabled: store.enabled,
        status: store.enabled ? { enabled: true, min_card_charge_micros: store.minCardChargeMicros, mailbox_monthly_micros: 3_000_000, domain_markup: "at_cost" } : undefined,
    }),
}));
vi.mock("./AIUsageCard", () => ({ default: () => null }));
vi.mock("./BalanceActivity", () => ({ default: () => null }));
vi.mock("./InvoicesSection", () => ({ default: () => null }));

import BalanceTab from "./BalanceTab";

function reload(over: Partial<AutoReload> = {}): AutoReload {
    return {
        enabled: false,
        threshold_usd: 5,
        amount_usd: 25,
        monthly_limit_usd: 250,
        reloaded_this_month_usd: 0,
        failure_code: null,
        failed_at: null,
        last_reload: null,
        retry_at: null,
        retry_reason: null,
        ...over,
    };
}

function balance(over: Partial<Balance> = {}): Balance {
    return {
        unlimited: false,
        can_add_money: true,
        balance_usd: 12,
        monthly_usd: 0,
        purchased_usd: 12,
        monthly_allowance_usd: 0,
        monthly_used_usd: 0,
        next_reset_at: null,
        top_up: { min_usd: 5, max_usd: 5000, presets_usd: [10, 25, 50, 100, 250], default_usd: 25 },
        auto_reload: reload(),
        payment_method: { brand: "visa", last4: "4242", exp_month: 12, exp_year: new Date().getFullYear() + 3 },
        ...over,
    };
}

async function open(b: Balance, href = "/app/settings/billing/balance") {
    api.getBalance.mockResolvedValue(b);
    api.getBalanceWithBurn.mockResolvedValue({ ...b, burn: { per_day_usd: 0, mailboxes_per_day_usd: 0, renewals_per_day_usd: 0, ai_per_day_usd: 0, mailboxes_monthly_usd: 0, renewals_due_usd: 0, billing_day: null } });
    const root = createRootRoute({ component: Outlet });
    const route = createRoute({ getParentRoute: () => root, path: "/app/settings/billing/balance", component: () => <BalanceTab isPaid /> });
    const router = createRouter({ routeTree: root.addChildren([route]), history: createMemoryHistory({ initialEntries: [href] }), parseSearch, stringifySearch });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(() => router.load());
    const view = render(
        <QueryClientProvider client={qc}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    await screen.findByRole("region", { name: "Balance" });
    return { router, ...view };
}

const dialog = () => screen.queryByRole("dialog");

beforeEach(() => {
    vi.clearAllMocks();
    store.enabled = false;
});

describe("Billing > Balance", () => {
    it("reads the daily draw only on this page, with its own request", async () => {
        await open(balance());
        await waitFor(() => expect(api.getBalanceWithBurn).toHaveBeenCalled());
        expect(api.getBalance).toHaveBeenCalled();
    });

    it("drops the banner's hash once the dialog opens, so the same link opens it again", async () => {
        const { router } = await open(balance(), "/app/settings/billing/balance#add-funds");
        await screen.findByRole("dialog");
        await waitFor(() => expect(router.state.location.hash).toBe(""));
        fireEvent.click(within(dialog()!).getByRole("button", { name: "Close" }));
        await waitFor(() => expect(dialog()).toBeNull());
        await act(() => router.navigate({ to: ".", hash: "add-funds" }));
        await screen.findByRole("dialog");
    });

    it("offers the server's presets, $25 default and $5 minimum while the store is off", async () => {
        await open(balance(), "/app/settings/billing/balance#add-funds");
        await screen.findByRole("dialog");
        const amounts = within(dialog()!).getAllByRole("radio").map((r) => r.textContent?.match(/\$[\d,]+/)?.[0]);
        expect(amounts).toEqual(["$10", "$25", "$50", "$100", "$250"]);
        expect(within(dialog()!).getByRole("radio", { checked: true })).toHaveTextContent("$25");
        expect(within(dialog()!).getByText(/^From \$5 to/)).toBeInTheDocument();
    });

    it("uses the store's $10 minimum and $100 default where the store is on", async () => {
        store.enabled = true;
        await open(balance(), "/app/settings/billing/balance#add-funds");
        await screen.findByRole("dialog");
        expect(within(dialog()!).getByRole("radio", { checked: true })).toHaveTextContent("$100");
        expect(within(dialog()!).getByText(/^From \$10 to/)).toBeInTheDocument();
    });

    it("keeps the auto-reload threshold floor at the server's minimum while the store is off", async () => {
        await open(balance(), "/app/settings/billing/balance#auto-reload");
        await screen.findByRole("dialog");
        const threshold = within(dialog()!).getByLabelText("When balance drops below");
        expect(threshold).toHaveValue(5);
        fireEvent.change(threshold, { target: { value: "0.5" } });
        fireEvent.blur(threshold);
        expect(threshold).toHaveValue(0.5);
        expect(within(dialog()!).queryByRole("status")).toBeNull();
    });

    it("lets a lapsed workspace see and turn off its auto top-up", async () => {
        api.updateAutoReload.mockImplementation(async (patch) => balance({ can_add_money: false, auto_reload: reload({ ...patch }) }));
        await open(balance({ can_add_money: false, auto_reload: reload({ enabled: true }) }));
        expect(screen.getByText(/Auto top-up adds \$25 \(charged \$25\.75 with the 3% processing fee\) when the balance drops below \$5\.00/)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Change" }));
        await screen.findByRole("dialog");
        fireEvent.click(within(dialog()!).getByRole("switch", { name: "Auto top-up" }));
        fireEvent.click(within(dialog()!).getByRole("button", { name: /Save/ }));
        await waitFor(() => expect(api.updateAutoReload).toHaveBeenCalledWith(expect.objectContaining({ enabled: false })));
    });

    it("still refuses to turn auto top-up on without a paid plan", async () => {
        await open(balance({ can_add_money: false }));
        expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
    });

    it("shows a soft decline's retry, the last top-up and this month's total", async () => {
        const retry = new Date(Date.now() + 3 * 86_400_000);
        await open(
            balance({
                auto_reload: reload({ enabled: true, retry_at: retry, reloaded_this_month_usd: 50, last_reload: { amount_usd: 25, at: new Date("2026-11-02T10:00:00Z") } }),
            }),
        );
        expect(screen.getByText(/The bank declined the last top-up for now/)).toBeInTheDocument();
        expect(screen.getByText("Last top-up Nov 2: $25.00")).toBeInTheDocument();
        expect(screen.getByText("$50.00 of $250 this month")).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Update card" }));
        await screen.findByRole("heading", { name: "Payment method" });
    });

    it("warns before the saved card expires", async () => {
        const now = new Date();
        await open(balance({ payment_method: { brand: "visa", last4: "4242", exp_month: now.getMonth() + 1, exp_year: now.getFullYear() } }));
        expect(screen.getByText(/expires .*Replace it before a top-up fails/)).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Replace card" })).toBeInTheDocument();
    });

    it("puts an Update card button on a stopped auto top-up", async () => {
        await open(balance({ auto_reload: reload({ failure_code: "card_declined", failed_at: new Date("2026-11-02T10:00:00Z") }) }));
        expect(screen.getByText(/Auto top-up stopped/)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Update card" }));
        await screen.findByRole("heading", { name: "Payment method" });
    });

    it("offers paying another way with a saved card", async () => {
        api.createBalanceCheckout.mockResolvedValue({ checkout_url: "about:blank" });
        await open(balance(), "/app/settings/billing/balance#add-funds");
        await screen.findByRole("dialog");
        fireEvent.click(within(dialog()!).getByRole("button", { name: "Pay another way" }));
        await waitFor(() => expect(api.createBalanceCheckout).toHaveBeenCalledWith(25));
    });
});
