import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useUpgradeFlow from "./useUpgradeFlow";

const state = vi.hoisted(() => ({
    subscription: { data: { stripe_customer_id: "", stripe_subscription_id: null as string | null, status: "incomplete", managed: true }, isPending: false, isError: false },
    plans: { data: [{ id: "grow-id", name: "Grow", stripe_price_id: "price_month", stripe_price_id_yearly: "price_year" }], isPending: false },
    checkout: vi.fn(),
    change: vi.fn(),
    portal: vi.fn(),
    error: vi.fn(),
}));
vi.mock("@/lib/api/hooks/app/subscription/useSubscription", () => ({ default: () => state.subscription }));
vi.mock("@/lib/api/hooks/app/subscription/usePlans", () => ({ default: () => state.plans }));
vi.mock("@/lib/api/hooks/app/subscription/useCreateCheckoutSession", () => ({ default: () => ({ mutateAsync: state.checkout }) }));
vi.mock("@/lib/api/hooks/app/subscription/useChangePlan", () => ({ default: () => ({ mutateAsync: state.change }) }));
vi.mock("@/lib/api/hooks/app/subscription/useCreatePortalSession", () => ({ default: () => ({ mutateAsync: state.portal, isPending: false }) }));
vi.mock("react-hot-toast/headless", () => ({ default: { promise: (p: Promise<unknown>) => p, error: state.error } }));

beforeEach(() => {
    vi.clearAllMocks();
    state.subscription.data = { stripe_customer_id: "", stripe_subscription_id: null, status: "incomplete", managed: true };
    state.subscription.isPending = false;
    state.subscription.isError = false;
    state.plans.data[0].stripe_price_id_yearly = "price_year";
    state.checkout.mockRejectedValue(new Error("stop before redirect"));
    state.change.mockResolvedValue({});
});

describe("billing flow selection", () => {
    it("sends a managed workspace without a Stripe subscription to checkout", async () => {
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "annual" }); });
        expect(state.checkout).toHaveBeenCalledWith(expect.objectContaining({ price_id: "price_year" }));
        expect(state.change).not.toHaveBeenCalled();
        expect(state.portal).not.toHaveBeenCalled();
    });
    it("sends a workspace with no subscription yet to checkout", async () => {
        state.subscription.data = null as unknown as typeof state.subscription.data;
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "monthly" }); });
        expect(state.checkout).toHaveBeenCalledWith(expect.objectContaining({ price_id: "price_month" }));
        expect(state.change).not.toHaveBeenCalled();
        expect(state.error).not.toHaveBeenCalled();
    });
    it("changes an existing Stripe subscription in place", async () => {
        state.subscription.data = { stripe_customer_id: "cus_existing", stripe_subscription_id: "sub_existing", status: "active", managed: false };
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "annual" }); });
        expect(state.change).toHaveBeenCalledWith(expect.objectContaining({ plan_id: "grow-id", interval: "year" }));
        expect(state.checkout).not.toHaveBeenCalled();
    });
    it("uses checkout again after a subscription has ended", async () => {
        state.subscription.data.stripe_subscription_id = "sub_canceled";
        state.subscription.data.status = "canceled";
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "annual" }); });
        expect(state.checkout).toHaveBeenCalled();
        expect(state.change).not.toHaveBeenCalled();
    });
    it("does not send missing prices to the portal", async () => {
        state.plans.data[0].stripe_price_id_yearly = "";
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "annual" }); });
        expect(state.checkout).not.toHaveBeenCalled();
        expect(state.portal).not.toHaveBeenCalled();
        expect(state.error).toHaveBeenCalled();
    });
    it("never requests a portal for a workspace without a customer", async () => {
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { expect(await result.current.openPortal()).toBe(false); });
        expect(state.portal).not.toHaveBeenCalled();
        expect(result.current.hasBillingCustomer).toBe(false);
    });
    it("does not start checkout when the existing subscription cannot be loaded", async () => {
        state.subscription.isError = true;
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "annual" }); });
        expect(state.checkout).not.toHaveBeenCalled();
        expect(state.change).not.toHaveBeenCalled();
        expect(state.error).toHaveBeenCalled();
    });
    it("waits for subscription state before choosing a billing operation", async () => {
        state.subscription.isPending = true;
        const { result } = renderHook(() => useUpgradeFlow());
        await act(async () => { await result.current.upgrade("grow", { interval: "annual" }); });
        expect(state.checkout).not.toHaveBeenCalled();
        expect(state.change).not.toHaveBeenCalled();
    });
});
