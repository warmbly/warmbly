import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { parseSearch, stringifySearch } from "@/lib/routerSearch";

const list = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client/app/subscription/getCreditTransactions", () => ({ default: list }));
const store = vi.hoisted(() => ({ enabled: false }));
vi.mock("@/lib/api/hooks/app/store/useStore", () => ({
    useStoreEnabled: () => ({ enabled: store.enabled, status: store.enabled ? { enabled: true } : undefined }),
}));

import BalanceActivity from "./BalanceActivity";

const line = (reason: string) => ({ id: reason, reason, amount_usd: -1, balance_after_usd: 10, quantity: 1, created_at: "2026-11-02T10:00:00Z" });

function page(category: string | undefined, storeLines: boolean) {
    const data = category === "store" ? (storeLines ? [line("store_mailbox_month")] : []) : [line("writing_assistant")];
    return { data, pagination: { next_cursor: null } };
}

async function open(storeLines: boolean, href = "/app/settings/billing/balance") {
    list.mockImplementation(async (_limit: number, _cursor: string | undefined, category?: string) => page(category, storeLines));
    const root = createRootRoute({ component: Outlet });
    const route = createRoute({ getParentRoute: () => root, path: "/app/settings/billing/balance", component: BalanceActivity });
    const other = createRoute({ getParentRoute: () => root, path: "/app/emails/buy/orders", component: () => null });
    const router = createRouter({ routeTree: root.addChildren([route, other]), history: createMemoryHistory({ initialEntries: [href] }), parseSearch, stringifySearch });
    await act(() => router.load());
    render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    await screen.findByText("Writing assistant");
}

beforeEach(() => {
    list.mockReset();
    store.enabled = false;
});

describe("Balance activity filter", () => {
    it("hides the Store filter while the store is off and the workspace never bought there", async () => {
        await open(false);
        await waitFor(() => expect(list).toHaveBeenCalledWith(1, undefined, "store"));
        expect(screen.queryByRole("radiogroup", { name: "Show activity" })).toBeNull();
    });

    it("shows it while the store is off for a workspace with store lines", async () => {
        await open(true);
        expect(await screen.findByRole("radio", { name: "Store" })).toBeInTheDocument();
    });

    it("shows it where the store is on, without probing for store lines", async () => {
        store.enabled = true;
        await open(false);
        expect(screen.getByRole("radio", { name: "Store" })).toBeInTheDocument();
        expect(list).not.toHaveBeenCalledWith(1, undefined, "store");
    });
});
