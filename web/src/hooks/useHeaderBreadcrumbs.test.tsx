import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Link, Outlet, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { hrefTarget } from "@/lib/routerSearch";
import { useHeaderBreadcrumbs } from "./useHeaderBreadcrumbs";

function Breadcrumbs() {
    const crumbs = useHeaderBreadcrumbs();
    return <nav aria-label="Breadcrumb">
        {crumbs.map(({ label, to, current }) => current
            ? <span key={to} aria-current="page">{label}</span>
            : <Link key={to} {...hrefTarget(to)}>{label}</Link>)}
    </nav>;
}

async function open(path: string, href: string) {
    const root = createRootRoute({ component: Outlet });
    const route = createRoute({ getParentRoute: () => root, path, component: Breadcrumbs });
    const router = createRouter({
        routeTree: root.addChildren([route]),
        history: createMemoryHistory({ initialEntries: [href] }),
        scrollRestoration: false,
    });
    await act(() => router.load());
    render(<RouterProvider router={router} />);
    return router;
}

beforeEach(() => vi.spyOn(window, "scrollTo").mockImplementation(() => {}));
afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
});

describe("header breadcrumbs", () => {
    it.each([
        "/app/unibox",
        "/app/unibox/all",
        "/app/unibox/account-123/AAQkAGFiNzBmMDQ2LWM1ZmUtNGYyNC05NTk5LTg0NjFmZGFhMTc5ZgAQAA_m_very_long_provider_message_id",
        "/app/unibox/label%3Aimportant/thread-123",
        "/app/unibox/app/ordinary-id",
    ])("shows only Inbox for %s, never scope or thread parameters", async (href) => {
        await open("/app/unibox/{-$scope}/{-$threadId}", href);
        expect(screen.getByRole("navigation")).toHaveTextContent(/^Inbox$/);
        if (href === "/app/unibox") {
            expect(screen.getByText("Inbox")).toHaveAttribute("aria-current", "page");
        } else {
            expect(screen.getByRole("link", { name: "Inbox" })).toHaveAttribute("href", "/app/unibox");
        }
    });

    it.each(["campaign-slug", "00000000-0000-0000-0000-000000000001", "app", "a%2Fb"])("preserves %s in nested link targets without displaying it", async (id) => {
        await open("/app/campaigns/$id/leads/details", `/app/campaigns/${id}/leads/details`);
        expect(screen.getByRole("link", { name: "Campaigns" })).toHaveAttribute("href", "/app/campaigns");
        expect(screen.getByRole("link", { name: "Leads" })).toHaveAttribute("href", `/app/campaigns/${id}/leads`);
        expect(screen.getByText("Details")).toHaveAttribute("aria-current", "page");
        expect(screen.getByRole("navigation")).toHaveTextContent(/^CampaignsLeadsDetails$/);
    });

    it("updates navigation when the matched route parameters change", async () => {
        const router = await open("/app/campaigns/$id/leads/details", "/app/campaigns/first/leads/details");
        const to: string = "/app/campaigns/second/leads/details";
        await act(() => router.navigate({ to }));
        await waitFor(() => expect(screen.getByRole("link", { name: "Leads" })).toHaveAttribute("href", "/app/campaigns/second/leads"));
    });

    it("routes the batches ancestor to the existing tab", async () => {
        await open("/app/placement/batches/$id", "/app/placement/batches/batch-id");
        expect(screen.getByRole("link", { name: "Placement tests" })).toHaveAttribute("href", "/app/placement");
        expect(screen.getByRole("link", { name: "Batches" })).toHaveAttribute("href", "/app/placement?tab=batches");
        expect(screen.getByRole("navigation")).toHaveTextContent(/^Placement testsBatches$/);
    });

    it("hides splat contents even when they span several segments", async () => {
        await open("/app/integrations/$", "/app/integrations/store/category/item-id");
        expect(screen.getByRole("navigation")).toHaveTextContent(/^Integrations$/);
        expect(screen.getByRole("link", { name: "Integrations" })).toHaveAttribute("href", "/app/integrations");
    });

    it("uses readable static labels and marks trailing-slash pages current", async () => {
        await open("/app/settings/ai-skills", "/app/settings/ai-skills/");
        expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/app/settings");
        expect(screen.getByText("AI skills")).toHaveAttribute("aria-current", "page");
    });

    it("makes unmapped hyphenated page names readable", async () => {
        await open("/app/settings/custom-page", "/app/settings/custom-page");
        expect(screen.getByText("Custom page")).toHaveAttribute("aria-current", "page");
    });
});
