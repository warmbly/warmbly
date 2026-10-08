import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import type { PlacementHost, PlacementProvider } from "@/lib/api/models/app/analytics/WarmupPlacement";
import { ProviderBreakdown } from "./PlacementCharts";

const counts = { sent: 10, delivered: 10, inbox: 8, tabs: 0, spam: 2, rescued: 0, unconfirmed: 0, inbox_rate: 80, spam_rate: 20 };
const providers: PlacementProvider[] = [{
    ...counts, group: "google", hosts: ["gmail", "google_workspace"].map((host): PlacementHost => ({ ...counts, host })),
}];
function Breakdown({ scope = "emails.mailbox.a.placement", loading = false }: { scope?: string; loading?: boolean }) {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}><ProviderBreakdown browseScope={scope} providers={loading ? [] : providers} /></UserContext.Provider>;
}

describe("provider expansion browse persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores expansion after remount and metadata loading, isolated from other placement panels", () => {
        const panel = render(<Breakdown />);
        fireEvent.click(screen.getByRole("button"));
        panel.unmount();
        const refreshed = render(<Breakdown loading />);
        refreshed.rerender(<Breakdown />);
        expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "true");
        refreshed.rerender(<Breakdown scope="deliverability.placement" />);
        expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "false");
        refreshed.rerender(<Breakdown />);
        expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "true");
        fireEvent.click(screen.getByRole("button"));
        refreshed.unmount();
        render(<Breakdown />);
        expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "false");
    });
});
