import type { PropsWithChildren } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useAccountStatuses, { stableChunks } from "@/lib/api/hooks/app/analytics/useAccountStatuses";
import getAccountStatuses from "@/lib/api/client/app/analytics/getAccountStatuses";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";

vi.mock("@/lib/api/client/app/analytics/getAccountStatuses", () => ({ default: vi.fn() }));

function harness() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: Infinity } } });
    const wrapper = ({ children }: PropsWithChildren) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    return { client, wrapper };
}

function status(id: string): AccountStatus {
    return { id } as AccountStatus;
}

beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getAccountStatuses).mockImplementation(async (ids) => ids.map(status));
});

describe("account status aggregation", () => {
    it("keeps sorted, bounded keys without mutating the input or refetching on reorder", async () => {
        const { client, wrapper } = harness();
        const ids = Array.from({ length: 205 }, (_, i) => String(204 - i).padStart(3, "0"));
        const original = [...ids];
        const { result, rerender, unmount } = renderHook(({ ids }) => useAccountStatuses(ids), { wrapper, initialProps: { ids } });
        await waitFor(() => expect(result.current.data).toHaveLength(205));
        expect(ids).toEqual(original);
        expect(vi.mocked(getAccountStatuses).mock.calls.map(([chunk]) => chunk.length)).toEqual([100, 100, 5]);
        expect(result.current.data.map((s) => s.id)).toEqual([...ids].sort());
        expect(getAccountStatuses).toHaveBeenCalledTimes(3);
        const first = result.current;
        rerender({ ids: [...ids].reverse() });
        expect(result.current).toBe(first);
        expect(getAccountStatuses).toHaveBeenCalledTimes(3);
        expect(client.getQueryCache().getAll().every((q) => q.queryKey.slice(0, 3).join("/") === "analytics/accounts/list")).toBe(true);
        unmount(); client.clear();
    });

    it("keeps loaded chunks when later list pages arrive", async () => {
        const { client, wrapper } = harness();
        const all = Array.from({ length: 400 }, (_, i) => String(i).padStart(3, "0"));
        const firstPage = all.filter((_, i) => i % 2 === 0);
        const { result, rerender, unmount } = renderHook(({ ids }) => useAccountStatuses(ids), { wrapper, initialProps: { ids: firstPage } });
        await waitFor(() => expect(result.current.data).toHaveLength(200));
        expect(getAccountStatuses).toHaveBeenCalledTimes(2);
        rerender({ ids: all });
        await waitFor(() => expect(result.current.data).toHaveLength(400));
        // Only the second page's ids are asked for; the first page's chunks are reused.
        expect(getAccountStatuses).toHaveBeenCalledTimes(4);
        expect(vi.mocked(getAccountStatuses).mock.calls.slice(2).flatMap(([ids]) => ids).sort()).toEqual(all.filter((_, i) => i % 2 === 1));
        unmount(); client.clear();
    });

    it("re-packs chunks once they fragment", () => {
        let chunks: string[][] = [];
        const ids: string[] = [];
        for (let i = 0; i < 10; i++) {
            ids.push(`m${i}`);
            chunks = stableChunks(chunks, ids);
        }
        expect(chunks.length).toBeLessThanOrEqual(3);
        expect(chunks.flat().sort()).toEqual([...ids].sort());
        expect(stableChunks(chunks, ids.slice(0, 5)).flat().sort()).toEqual(ids.slice(0, 5));
    });

    it("skips per-row aggregation on unchanged renders and recomputes for realtime data", async () => {
        const { client, wrapper } = harness();
        const ids = Array.from({ length: 200 }, (_, i) => String(i));
        const { result, rerender, unmount } = renderHook(() => useAccountStatuses(ids), { wrapper });
        await waitFor(() => expect(result.current.data).toHaveLength(200));
        const first = result.current;
        const query = client.getQueryCache().getAll()[0];
        // Every aggregation maps each chunk into observations.
        const observationMaps = client.getQueryCache().getAll().map((q) => vi.spyOn(q.state.data as AccountStatus[], "map"));
        const start = performance.now();
        for (let i = 0; i < 200; i++) rerender();
        console.info(JSON.stringify({ fixture: "200 statuses; two 100-row chunks; 200 unchanged renders", observationMaps: observationMaps.reduce((sum, spy) => sum + spy.mock.calls.length, 0), elapsedMs: performance.now() - start }));
        expect(result.current).toBe(first);
        for (const spy of observationMaps) {
            expect(spy).not.toHaveBeenCalled();
            spy.mockRestore();
        }
        act(() => client.setQueryData(query.queryKey, [status("updated")]));
        await waitFor(() => expect(result.current.data[0].id).toBe("updated"));
        expect(result.current.observations[0]).toMatchObject({ status: status("updated"), current: true });
        unmount(); client.clear();
    });

    it("preserves enabled gating, empty selection and source-revision freshness", async () => {
        const { client, wrapper } = harness();
        const { result, rerender, unmount } = renderHook(
            ({ ids, enabled, revision }) => useAccountStatuses(ids, { enabled, sourceRevision: revision }),
            { wrapper, initialProps: { ids: [] as string[], enabled: false, revision: 0 } },
        );
        expect(result.current.data).toEqual([]);
        expect(getAccountStatuses).not.toHaveBeenCalled();
        rerender({ ids: ["a"], enabled: false, revision: 0 });
        expect(getAccountStatuses).not.toHaveBeenCalled();
        rerender({ ids: ["a"], enabled: true, revision: 0 });
        await waitFor(() => expect(result.current.data).toEqual([status("a")]));
        rerender({ ids: ["b"], enabled: true, revision: 1 });
        await waitFor(() => expect(result.current.data).toEqual([status("b")]));
        expect(getAccountStatuses).toHaveBeenCalledTimes(2);
        expect(client.getQueryCache().getAll().some((q) => q.queryKey[4] === 1)).toBe(true);
        unmount(); client.clear();
    });

    it("keeps the same data array when a refetch returns the same statuses", async () => {
        const { client, wrapper } = harness();
        const ids = Array.from({ length: 150 }, (_, i) => String(i));
        const { result, unmount } = renderHook(() => useAccountStatuses(ids), { wrapper });
        await waitFor(() => expect(result.current.data).toHaveLength(150));
        await waitFor(() => expect(result.current.observations.every((o) => o.current)).toBe(true));
        const data = result.current.data;
        let release!: () => void;
        const gate = new Promise<void>((resolve) => { release = resolve; });
        vi.mocked(getAccountStatuses).mockImplementation(async (chunkIds) => { await gate; return chunkIds.map(status); });
        act(() => { void client.invalidateQueries({ queryKey: ["analytics", "accounts"] }); });
        await waitFor(() => expect(result.current.isFetching).toBe(true));
        expect(result.current.data).toBe(data);
        act(() => release());
        await waitFor(() => expect(result.current.isFetching).toBe(false));
        expect(result.current.data).toBe(data);
        expect(result.current.observations.every((o) => o.current)).toBe(true);
        act(() => client.setQueryData(client.getQueryCache().getAll()[0].queryKey, [status("updated")]));
        await waitFor(() => expect(result.current.data).not.toBe(data));
        unmount(); client.clear();
    });

    it("marks retained observations non-current during refetch and on refetch failure", async () => {
        const { client, wrapper } = harness();
        const ids = ["a"];
        const { result, unmount } = renderHook(() => useAccountStatuses(ids), { wrapper });
        await waitFor(() => expect(result.current.observations[0]?.current).toBe(true));
        const observedAt = result.current.observations[0].observedAt;
        let reject!: (reason: Error) => void;
        vi.mocked(getAccountStatuses).mockImplementationOnce(() => new Promise((_resolve, rej) => { reject = rej; }));
        act(() => { void client.invalidateQueries({ queryKey: ["analytics", "accounts"] }); });
        await waitFor(() => expect(result.current.isFetching).toBe(true));
        expect(result.current.observations[0]).toMatchObject({ observedAt, current: false });
        act(() => reject(new Error("refetch failed")));
        await waitFor(() => expect(result.current.isError).toBe(true));
        expect(result.current.data).toEqual([status("a")]);
        expect(result.current.observations[0]).toMatchObject({ observedAt, current: false });
        unmount(); client.clear();
    });
});
