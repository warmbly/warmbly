import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AdminPerm } from "@/lib/auth/permissions";
import { INSTANCE_MONITORING_KEY, useInstanceMonitoring } from "./useInstanceMonitoring";

const state = vi.hoisted(() => ({ mask: 0, id: "first-admin", fetch: vi.fn() }));
vi.mock("@/hooks/useMe", () => ({ useMe: () => ({ data: { id: state.id, admin_permissions: state.mask } }) }));
vi.mock("@/lib/api/client/admin/monitoring", () => ({ getInstanceMonitoring: state.fetch }));

let root: Root | undefined;
let client: QueryClient | undefined;
afterEach(async () => {
    if (root) await act(async () => root!.unmount());
    root = undefined;
    client?.clear();
    vi.clearAllMocks();
});

function Reader() {
    const query = useInstanceMonitoring();
    return <span>{query.data?.checked_at ?? "No snapshot"}</span>;
}

describe("shared monitoring query", () => {
    it("shares one scoped cache between readers, keeps original clocks and bounds polling", async () => {
        state.mask = AdminPerm.ViewAnalytics;
        state.id = "first-admin";
        state.fetch.mockResolvedValue({ version: "1", checked_at: "original-server-observation", sources: [] });
        client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        const container = document.createElement("div");
        root = createRoot(container);
        await act(async () => root!.render(<QueryClientProvider client={client!}><Reader /><Reader /></QueryClientProvider>));
        await vi.waitFor(() => expect(state.fetch).toHaveBeenCalledTimes(1));
        const queries = client.getQueryCache().getAll();
        expect(queries).toHaveLength(1);
        expect(queries[0].queryKey).toEqual([...INSTANCE_MONITORING_KEY, "first-admin", AdminPerm.ViewAnalytics]);
        expect(queries[0].state.data).toMatchObject({ checked_at: "original-server-observation" });
        expect(queries[0].options).toMatchObject({ staleTime: 60_000, refetchInterval: 60_000, refetchIntervalInBackground: false, refetchOnWindowFocus: false, retry: false });
    });

    it("does not fetch without analytics and does not reuse a higher-permission or different-user snapshot", async () => {
        client = new QueryClient();
        client.setQueryData([...INSTANCE_MONITORING_KEY, "first-admin", AdminPerm.ViewAnalytics | AdminPerm.ViewUsers], { checked_at: "private-old-observation" });
        client.setQueryData([...INSTANCE_MONITORING_KEY, "other-admin", AdminPerm.ViewAnalytics], { checked_at: "private-other-admin" });
        state.mask = 0;
        state.id = "first-admin";
        const container = document.createElement("div");
        root = createRoot(container);
        await act(async () => root!.render(<QueryClientProvider client={client!}><Reader /></QueryClientProvider>));
        expect(state.fetch).not.toHaveBeenCalled();
        expect(container.textContent).toBe("No snapshot");
        state.mask = AdminPerm.ViewAnalytics;
        state.fetch.mockResolvedValue({ checked_at: "new-projection" });
        await act(async () => root!.render(<QueryClientProvider client={client!}><Reader /></QueryClientProvider>));
        await vi.waitFor(() => expect(state.fetch).toHaveBeenCalledTimes(1));
        expect(client.getQueryData([...INSTANCE_MONITORING_KEY, "first-admin", AdminPerm.ViewAnalytics])).toMatchObject({ checked_at: "new-projection" });
    });
});
