import type { PropsWithChildren } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { focusManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import getAccountStatus from "@/lib/api/client/app/analytics/getAccountStatus";
import getAccountStatuses from "@/lib/api/client/app/analytics/getAccountStatuses";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import useAccountStatus from "./useAccountStatus";
import useAccountStatuses from "./useAccountStatuses";

vi.mock("@/lib/api/client/app/analytics/getAccountStatus", () => ({ default: vi.fn() }));
vi.mock("@/lib/api/client/app/analytics/getAccountStatuses", () => ({ default: vi.fn() }));

function mailbox(recovered: boolean): AccountStatus {
    return {
        id: "mailbox", email: "mailbox@example.test", provider: "smtp_imap", status: "active",
        last_synced_at: recovered ? new Date() : new Date(Date.now() - 3 * 60 * 60_000),
        health: { status: recovered ? "healthy" : "warning", score: recovered ? 100 : 80 },
        errors: recovered ? [] : [{
            id: "failure", error_code: "SERVER_UNREACHABLE", severity: "WARNING", operation: "sync",
            title: "Mail server sync connection failed", message: "Automatic retry", created_at: new Date(),
        }],
        daily_usage: { date: "2026-11-01", campaign_sent: 0, campaign_limit: 10 }, in_campaign: false,
    };
}

function harness() {
    const client = new QueryClient({ defaultOptions: { queries: {
        retry: false, gcTime: Infinity, staleTime: 30_000, refetchOnWindowFocus: false,
    } } });
    const wrapper = ({ children }: PropsWithChildren) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const ids = ["mailbox"];
    const hook = renderHook(({ enabled }) => ({
        drawer: useAccountStatus(enabled ? "mailbox" : ""), list: useAccountStatuses(ids, { enabled }),
    }), { wrapper, initialProps: { enabled: true } });
    return { client, ...hook };
}

beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    focusManager.setFocused(true);
    vi.mocked(getAccountStatus).mockResolvedValue(mailbox(false));
    vi.mocked(getAccountStatuses).mockResolvedValue([mailbox(false)]);
});

afterEach(() => {
    cleanup();
    focusManager.setFocused(undefined);
    vi.useRealTimers();
});

describe("mailbox status freshness without realtime events", () => {
    it("replaces a recovered warning and its old success time in both open views", async () => {
        const { client, result, unmount } = harness();
        await act(() => vi.advanceTimersByTimeAsync(1));
        expect(result.current.drawer.data?.errors).toHaveLength(1);
        expect(result.current.list.data[0]?.errors).toHaveLength(1);

        const recovered = mailbox(true);
        vi.mocked(getAccountStatus).mockResolvedValue(recovered);
        vi.mocked(getAccountStatuses).mockResolvedValue([recovered]);
        await act(() => vi.advanceTimersByTimeAsync(59_000));
        expect(getAccountStatus).toHaveBeenCalledTimes(1);
        expect(getAccountStatuses).toHaveBeenCalledTimes(1);
        await act(() => vi.advanceTimersByTimeAsync(1_001));
        expect(result.current.drawer.data?.errors).toEqual([]);
        expect(result.current.drawer.data?.last_synced_at).toEqual(recovered.last_synced_at);
        expect(result.current.list.data[0]?.errors).toEqual([]);
        expect(result.current.list.data[0]?.last_synced_at).toEqual(recovered.last_synced_at);
        unmount(); client.clear();
    });

    it("retains a reported failure until the server confirms recovery, and stops after unmount", async () => {
        const { client, result, unmount } = harness();
        await act(() => vi.advanceTimersByTimeAsync(60_002));
        expect(getAccountStatus).toHaveBeenCalledTimes(2);
        expect(getAccountStatuses).toHaveBeenCalledTimes(2);
        expect(result.current.drawer.data?.errors).toHaveLength(1);
        expect(result.current.list.data[0]?.errors).toHaveLength(1);
        unmount();
        await act(() => vi.advanceTimersByTimeAsync(120_000));
        expect(getAccountStatus).toHaveBeenCalledTimes(2);
        expect(getAccountStatuses).toHaveBeenCalledTimes(2);
        client.clear();
    });

    it("pauses periodic reads while hidden and refreshes immediately on return", async () => {
        const { client, result, unmount } = harness();
        await act(() => vi.advanceTimersByTimeAsync(1));
        act(() => focusManager.setFocused(false));
        const recovered = mailbox(true);
        vi.mocked(getAccountStatus).mockResolvedValue(recovered);
        vi.mocked(getAccountStatuses).mockResolvedValue([recovered]);
        await act(() => vi.advanceTimersByTimeAsync(60_001));
        expect(getAccountStatus).toHaveBeenCalledTimes(1);
        expect(getAccountStatuses).toHaveBeenCalledTimes(1);
        act(() => focusManager.setFocused(true));
        await act(() => vi.advanceTimersByTimeAsync(1));
        expect(result.current.drawer.data?.errors).toEqual([]);
        expect(result.current.list.data[0]?.errors).toEqual([]);
        unmount(); client.clear();
    });

    it("keeps the last report when a refresh fails instead of assuming recovery", async () => {
        const { client, result, unmount } = harness();
        await act(() => vi.advanceTimersByTimeAsync(1));
        vi.mocked(getAccountStatus).mockRejectedValue(new Error("Status unavailable"));
        vi.mocked(getAccountStatuses).mockRejectedValue(new Error("Status unavailable"));
        await act(() => vi.advanceTimersByTimeAsync(60_001));
        expect(result.current.drawer.isError).toBe(true);
        expect(result.current.list.isError).toBe(true);
        expect(result.current.drawer.data?.errors).toHaveLength(1);
        expect(result.current.list.data[0]?.errors).toHaveLength(1);
        expect(result.current.list.observations[0]?.current).toBe(false);
        unmount(); client.clear();
    });

    it("respects disabled status reads during timer and focus changes", async () => {
        const { client, rerender, unmount } = harness();
        await act(() => vi.advanceTimersByTimeAsync(1));
        rerender({ enabled: false });
        act(() => focusManager.setFocused(false));
        await act(() => vi.advanceTimersByTimeAsync(60_001));
        act(() => focusManager.setFocused(true));
        await act(() => vi.advanceTimersByTimeAsync(60_001));
        expect(getAccountStatus).toHaveBeenCalledTimes(1);
        expect(getAccountStatuses).toHaveBeenCalledTimes(1);
        unmount(); client.clear();
    });
});
