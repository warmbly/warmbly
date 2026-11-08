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

describe("provider rows below the share floor", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("shows counts and one square per receipt, not a 50% spam share, for two receipts", () => {
        const two = { ...counts, sent: 2, delivered: 2, inbox: 1, spam: 1, inbox_rate: 50, spam_rate: 50 };
        const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
        render(<UserContext.Provider value={profile}><ProviderBreakdown browseScope="x" providers={[{ ...two, group: "google", hosts: [] }]} /></UserContext.Provider>);
        expect(screen.queryByText(/50%/)).toBeNull();
        expect(screen.getByText("1 spam")).toBeInTheDocument();
        expect(screen.getByText("1 non-spam")).toBeInTheDocument();
        const bar = screen.getByTitle(/A share is shown after 10 classified receipts/);
        expect(bar.children).toHaveLength(10);
        expect(bar.querySelectorAll(".bg-emerald-500")).toHaveLength(1);
        expect(bar.querySelectorAll(".bg-rose-500")).toHaveLength(1);
        expect(bar.querySelectorAll(".bg-slate-100")).toHaveLength(8);
    });

    it("withholds an expanded host's share below the floor while its provider shows one", () => {
        const host = { ...counts, sent: 2, delivered: 2, inbox: 0, tabs: 2, spam: 0, inbox_rate: 100, spam_rate: 0 };
        const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
        render(<UserContext.Provider value={profile}><ProviderBreakdown browseScope="z" providers={[{ ...counts, group: "google", hosts: [{ ...counts, host: "google_workspace" }, { ...host, host: "gmail" }] }]} /></UserContext.Provider>);
        fireEvent.click(screen.getByRole("button"));
        const row = screen.getByText("2 non-spam").parentElement!.parentElement!;
        expect(row).toHaveTextContent("0 spam");
        expect(row).not.toHaveTextContent("%");
    });

    it("shows shares once the floor is met", () => {
        const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
        render(<UserContext.Provider value={profile}><ProviderBreakdown browseScope="y" providers={[{ ...counts, group: "google", hosts: [] }]} /></UserContext.Provider>);
        expect(screen.getByText("80%")).toBeInTheDocument();
        expect(screen.getByText("20% spam")).toBeInTheDocument();
    });
});
