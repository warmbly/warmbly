// Reorder a campaign's sequence steps. The hook commits the whole new order to
// the server and, on success, invalidates the cached sequence list so the
// builder reflects the new order without waiting for the response to settle.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";

const reorderSteps = vi.fn();

vi.mock("@/lib/api/client/app/campaigns/sequences/reorderSteps", () => ({
    default: (...args: unknown[]) => reorderSteps(...args),
}));

const useReorderSteps = (await import("./useReorderSteps")).default;

const SEQ_KEY = ["campaigns", "camp-a", "sequences"] as const;

function wrapper(client: QueryClient) {
    return function Wrapper({ children }: { children: React.ReactNode }) {
        return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    };
}

describe("useReorderSteps commits the new order and refreshes the cache", () => {
    let client: QueryClient;

    beforeEach(() => {
        vi.clearAllMocks();
        client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
        client.setQueryData(SEQ_KEY, []);
    });

    it("sends the full ordered list of step IDs to the server", async () => {
        reorderSteps.mockResolvedValue(undefined);
        const { result } = renderHook(() => useReorderSteps("camp-a"), { wrapper: wrapper(client) });

        await result.current.mutateAsync(["c", "a", "b"]);

        expect(reorderSteps).toHaveBeenCalledTimes(1);
        expect(reorderSteps).toHaveBeenCalledWith("camp-a", ["c", "a", "b"]);
    });

    it("invalidates the sequence list after the server answers", async () => {
        reorderSteps.mockResolvedValue(undefined);
        const invalidate = vi.fn();
        vi.spyOn(client, "invalidateQueries").mockImplementation((...args: unknown[]) => {
            invalidate(...args);
            return Promise.resolve();
        });
        const { result } = renderHook(() => useReorderSteps("camp-a"), { wrapper: wrapper(client) });

        await result.current.mutateAsync(["c", "a", "b"]);

        await waitFor(() => expect(invalidate).toHaveBeenCalledTimes(1));
    });

    it("propagates a server rejection so the caller can react", async () => {
        reorderSteps.mockRejectedValue(new Error("step_order_cycle"));
        const { result } = renderHook(() => useReorderSteps("camp-a"), { wrapper: wrapper(client) });

        await expect(result.current.mutateAsync(["a", "b"])).rejects.toThrow("step_order_cycle");
    });
});
