import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import DeliverabilityPage from "./page";

vi.mock("@/lib/api/hooks/app/analytics/useDeliverability", () => ({ default: () => ({ data: undefined, isPending: true, refetch: vi.fn() }) }));
vi.mock("@/components/app/placement/WarmupPlacementSection", () => ({ default: () => null }));
vi.mock("@/components/app/advisor/AdvisorSummaryBar", () => ({ default: () => null }));
vi.mock("@tanstack/react-router", () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a> }));

function Page() {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}><DeliverabilityPage /></UserContext.Provider>;
}
const key = `${BROWSE_STORAGE_PREFIX}account-user:workspace:deliverability.view`;

describe("deliverability legacy browse preferences", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("seeds compatible legacy preferences once, deduplicates sections, and never dual-writes them", () => {
        vi.mocked(localStorage.getItem).mockReturnValue(JSON.stringify({ range: "30d", hidden: ["providers", "providers", "old"] }));
        const page = render(<Page />);
        expect(JSON.parse(sessionStorage.getItem(key)!).value).toEqual({ range: "30d", hidden: ["providers"] });
        fireEvent.click(screen.getByRole("button", { name: "90d" }));
        page.unmount();
        render(<Page />);
        expect(JSON.parse(sessionStorage.getItem(key)!).value).toEqual({ range: "90d", hidden: ["providers"] });
        expect(localStorage.setItem).not.toHaveBeenCalledWith("warmbly:deliverability-view", expect.any(String));
    });

    it("retains a scoped preference in preference to a global legacy value", () => {
        vi.mocked(localStorage.getItem).mockReturnValue(JSON.stringify({ range: "30d", hidden: ["chart"] }));
        sessionStorage.setItem(key, JSON.stringify({ value: { range: "90d", hidden: ["totals"] } }));
        render(<Page />);
        expect(JSON.parse(sessionStorage.getItem(key)!).value).toEqual({ range: "90d", hidden: ["totals"] });
        expect(localStorage.setItem).not.toHaveBeenCalledWith("warmbly:deliverability-view", expect.any(String));
    });
});
