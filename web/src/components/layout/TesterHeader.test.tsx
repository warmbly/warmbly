import type { PropsWithChildren } from "react";
import { fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { PlanPill } from "./PlanPill";
import { BalanceMeter } from "./BalanceMeter";

const fixture = vi.hoisted(() => ({
    subscription: {
        data: { managed: true, status: "incomplete", plan: { id: "00000000-0000-0000-0000-0000000000e1", name: "Test" } },
        isPending: false,
    },
}));

vi.mock("@tanstack/react-router", () => ({
    Link: ({ children, to, title, className }: PropsWithChildren<{ to: string; title?: string; className?: string }>) => (
        <a href={to} title={title} className={className}>{children}</a>
    ),
}));
vi.mock("@/lib/api/hooks/app/subscription/useSubscription", () => ({ default: () => fixture.subscription }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ data: { billing_enabled: true }, isLoading: false }) }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({}) }));
vi.mock("@/hooks/usePermission", () => ({ usePermission: () => true }));
vi.mock("@/hooks/useAiMetered", () => ({ default: () => true }));
vi.mock("@/lib/api/hooks/app/subscription/useBalance", () => ({
    useBalance: () => ({
        isPending: false,
        data: {
            unlimited: false,
            balance_usd: 5,
            monthly_usd: 5,
            purchased_usd: 0,
            monthly_allowance_usd: 5,
            monthly_used_usd: 0,
            next_reset_at: null,
            low_balance_usd: 0.5,
        },
    }),
}));
vi.mock("@/lib/api/hooks/app/subscription/useCreditUsage", () => ({ default: () => ({ isPending: false }) }));
vi.mock("@/components/ui/AnimatedNumber", () => ({
    default: ({ value, format }: { value: number; format?: (n: number) => string }) => <span>{format ? format(value) : value}</span>,
}));
vi.mock("@/components/ui/dither", () => ({ DitherMeter: () => <div /> }));

beforeEach(() => {
    fixture.subscription.data = { managed: true, status: "incomplete", plan: { id: "00000000-0000-0000-0000-0000000000e1", name: "Test" } };
    useAppStore.setState({ currentOrganization: { id: "review", name: "Reviewer", category: "test", role: "owner" } });
});
afterEach(() => useAppStore.setState({ currentOrganization: null }));

describe("dedicated tester header", () => {
    it("shows Test instead of a customer plan, even after the grant expires", () => {
        fixture.subscription.data.managed = false;
        fixture.subscription.data.plan.name = "Free";
        render(<PlanPill />);
        expect(screen.getByRole("link", { name: "Test" })).toHaveAttribute("title", "Dedicated tester workspace");
        expect(screen.queryByText("Free")).not.toBeInTheDocument();
    });

    it("labels the bounded allowance as test balance in the header and popover", () => {
        render(<BalanceMeter />);
        const button = screen.getByRole("button", { name: "$5.00 test balance left" });
        expect(button).toHaveTextContent("Test balance");
        fireEvent.click(button);
        expect(screen.queryByText("Included this cycle")).not.toBeInTheDocument();
        expect(screen.getAllByText("Test balance").length).toBeGreaterThan(1);
    });

    it("leaves a customer workspace's plan and balance labels unchanged", () => {
        useAppStore.setState({ currentOrganization: { id: "customer", name: "Customer", category: "standard", role: "owner" } });
        fixture.subscription.data = { managed: false, status: "active", plan: { id: "customer-plan", name: "Starter" } };
        render(<><PlanPill /><BalanceMeter /></>);
        expect(screen.getByRole("link", { name: "Starter" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "$5.00 balance left" })).toBeInTheDocument();
        expect(screen.queryByText("Test")).not.toBeInTheDocument();
    });

    it("unlocks paid surfaces from an active Test grant, never from the category alone", () => {
        const { result, rerender } = renderHook(() => useFeatureAccess());
        expect(result.current).toMatchObject({ paid: true, locked: false, hasTeam: true, hasBulkOps: true, hasAdvanced: true, hasWebhooks: true });
        fixture.subscription.data.managed = false;
        fixture.subscription.data.plan = { id: "free", name: "Free" };
        rerender();
        expect(result.current).toMatchObject({ paid: false, locked: true, hasTeam: false, hasBulkOps: false, hasAdvanced: false, hasWebhooks: false });
    });
});
