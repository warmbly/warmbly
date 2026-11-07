import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import useFeatureAccess, { useTrialDaysLeft } from "@/hooks/useFeatureAccess";

const DAY = 86_400_000;
const fixture = vi.hoisted(() => ({ subscription: { data: undefined as unknown, isPending: false } }));

vi.mock("@/lib/api/hooks/app/subscription/useSubscription", () => ({ default: () => fixture.subscription }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ data: { billing_enabled: true }, isLoading: false }) }));

beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-06T12:00:00Z"));
});
afterEach(() => vi.useRealTimers());

describe("trial countdown", () => {
    it("drops the day count at each boundary for every countdown at once", () => {
        const endsAt = new Date(Date.now() + 2.5 * DAY);
        const bar = renderHook(() => useTrialDaysLeft(endsAt));
        const banner = renderHook(() => useTrialDaysLeft(endsAt));
        expect([bar.result.current, banner.result.current]).toEqual([3, 3]);

        act(() => void vi.advanceTimersByTime(0.5 * DAY + 1000));
        expect([bar.result.current, banner.result.current]).toEqual([2, 2]);

        act(() => void vi.advanceTimersByTime(DAY));
        expect([bar.result.current, banner.result.current]).toEqual([1, 1]);
    });

    it("locks an open tab when the trial ends, with no new data", () => {
        fixture.subscription = {
            isPending: false,
            data: { status: "incomplete", free_trial_ends_at: new Date(Date.now() + DAY).toISOString() },
        };
        const { result } = renderHook(() => useFeatureAccess());
        expect(result.current).toMatchObject({ trial: true, locked: false, hasInbox: true });

        act(() => void vi.advanceTimersByTime(DAY + 1000));
        expect(result.current).toMatchObject({ trial: false, trialEnded: true, locked: true, hasInbox: false });
    });
});
