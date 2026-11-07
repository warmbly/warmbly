// Issue #207 (second report): clicking a campaign tab changed the URL without
// changing the page. This mounts the real dashboard shell (RootAppLayout ->
// AppShell -> RouteBoundary -> Suspense -> Outlet) around the real campaign
// routes and walks the tab bar, so a regression that leaves the content panel
// showing the previous tab, or blank, fails here instead of in someone's
// browser. It also pins the scroll reset: the shell scrolls an inner div, so
// nothing but AppShell can put a new route back at the top.

import React from "react";
import { describe, it, expect, vi } from "vitest";
import { screen, act, fireEvent } from "@testing-library/react";
import { createRootRoute, createRoute, Outlet } from "@tanstack/react-router";
import { createTestRouter, renderRouter } from "@/test/routerHarness";

// jsdom has no layout: scrollTop is a hard 0 and nothing has a height, so the
// shell would see a hidden panel and leave it alone. Give every element a
// height and a settable offset so the panel's position can be set and read.
const tops = new WeakMap<Element, number>();
Object.defineProperty(Element.prototype, "scrollTop", {
    configurable: true,
    get(this: Element) {
        return tops.get(this) ?? 0;
    },
    set(this: Element, value: number) {
        tops.set(this, value);
    },
});
Object.defineProperty(Element.prototype, "clientHeight", { configurable: true, get: () => 400 });
Object.defineProperty(Element.prototype, "scrollHeight", { configurable: true, get: () => 4000 });
(Element.prototype as unknown as { scrollTo: () => void }).scrollTo = () => {};

// The shell's content panel, the one scroll container every page renders into.
const contentPanel = () => document.querySelector<HTMLElement>("main div.h-full.overflow-auto")!;

// Every request the dashboard bootstrap fires, answered with the smallest
// shape each consumer needs. `hang` lets a test hold one endpoint open.
let hang: RegExp | null = null;
vi.mock("@/lib/api/client/Request", () => ({
    default: (cfg: { url?: string }) => {
        const url = String(cfg?.url ?? "");
        if (hang?.test(url)) return new Promise(() => {});
        return Promise.resolve(route(url));
    },
}));
vi.mock("@/lib/helper/getToken", () => ({
    default: () => ({
        access_token: "a",
        refresh_token: "r",
        access_token_expires_at: new Date(Date.now() + 3600e3).toISOString(),
        refresh_token_expires_at: new Date(Date.now() + 3600e3).toISOString(),
    }),
}));
// The real provider opens a websocket; the channel hooks are what components use.
vi.mock("@/hooks/SocketProvider", () => ({
    default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/hooks/context/socket", async (orig) => {
    const actual = (await orig()) as Record<string, unknown>;
    return {
        ...actual,
        useSocket: () => ({
            isConnected: false,
            subscribeToChannel: () => () => {},
            pushToChannel: () => {},
            socket: null,
            status: "closed",
        }),
        useChannel: () => ({ state: "closed", push: () => {}, channel: null }),
        useChannelEvent: () => {},
        useChannelSubscription: () => {},
    };
});

const EMPTY_LIST = { data: [], pagination: { total: 0, next_cursor: null, has_more: false } };

function route(url: string): unknown {
    if (/^\/campaigns\/[^/?]+$/.test(url)) {
        return { id: "camp-1", name: "Probe campaign", status: "draft", description: "" };
    }
    if (url.endsWith("/send-plan")) {
        return {
            campaign_id: "camp-1", status: "draft", day: "2026-09-19", timezone: "UTC",
            computed_at: new Date().toISOString(), configured_ceiling: 0, projected_today: 0,
            sent_today: 0, expected_remaining: 0, bottleneck: "", limits: [],
            window: { sending_day: true, open_now: true, minutes_left: 60 },
            leads: {
                due_now: 0, due_later_today: 0, new_leads_due_today: 0, waiting_on_step: 0,
                waiting_on_condition: 0, held: 0, waiting_on_sender: 0, new_leads_started_today: 0, max_new_leads_per_day: 0,
            },
            mailboxes: [],
        };
    }
    if (url === "/auth/me" || url === "/me") {
        return {
            id: "u1", email: "d@w.com", first_name: "D", last_name: "W",
            onboarding_completed_at: new Date().toISOString(),
            tags: [], categories: [], folders: [], roles: [],
        };
    }
    if (url.startsWith("/organization")) return [{ id: "org-1", name: "Org", slug: "org" }];
    if (url.startsWith("/subscription/credits/")) return { data: [] };
    if (url.startsWith("/subscription/credits")) {
        return {
            monthly_balance: 100, monthly_allowance: 100, purchased_balance: 0,
            spent_today: 0, spent_week: 0, spent_month: 0,
            balance_usd: 1, monthly_usd: 1, purchased_usd: 0, monthly_allowance_usd: 1,
        };
    }
    if (url.startsWith("/subscription/balance")) return { unlimited: true };
    if (url.startsWith("/subscription")) return { plan: { name: "Pro" }, status: "active" };
    if (url.startsWith("/analytics")) return { summary: {}, steps: [], data: [] };
    if (url.startsWith("/advisor")) return { findings: [], data: [], total: 0 };
    if (url.includes("/steps")) return [];
    if (url.includes("connections")) return { connections: [] };
    return EMPTY_LIST;
}

const RootAppLayout = (await import("../layout")).default;
const CampaignLayout = (await import("./[id]/layout")).default;
const CampaignOverview = (await import("./[id]/page")).default;
const CampaignLeads = (await import("./[id]/leads/page")).default;
const CampaignSteps = (await import("./[id]/steps/page")).default;

async function mountDashboard() {
    const root = createRootRoute({ component: Outlet });
    const app = createRoute({ getParentRoute: () => root, path: "app", component: RootAppLayout });
    const campaigns = createRoute({ getParentRoute: () => app, path: "campaigns" });
    const campaign = createRoute({ getParentRoute: () => campaigns, path: "$id", component: CampaignLayout });
    const overview = createRoute({ getParentRoute: () => campaign, path: "/", component: CampaignOverview });
    const leads = createRoute({ getParentRoute: () => campaign, path: "leads", component: CampaignLeads });
    const steps = createRoute({ getParentRoute: () => campaign, path: "steps", component: CampaignSteps });
    const router = createTestRouter(
        root.addChildren([app.addChildren([campaigns.addChildren([campaign.addChildren([overview, leads, steps])])])]),
        "/app/campaigns/camp-1",
    );
    await renderRouter(router);
    return router;
}

async function settle() {
    await act(async () => {
        await new Promise((r) => setTimeout(r, 300));
    });
}

function clickTab(name: RegExp) {
    const links = screen.getAllByRole("link", { name });
    return act(async () => {
        fireEvent.click(links[links.length - 1]);
    });
}

// The whole shell is slow to mount in jsdom on a loaded runner, so the suite gets more than the 5s default.
describe("campaign tabs", { timeout: 30_000 }, () => {
    it("swaps the content panel when the URL changes", async () => {
        hang = null;
        const router = await mountDashboard();
        await settle();
        expect(screen.queryByText("Performance")).toBeTruthy();

        await clickTab(/Leads/i);
        await settle();
        expect(router.state.location.pathname).toBe("/app/campaigns/camp-1/leads");
        // The Overview is gone and the Leads browser is mounted in its place.
        expect(screen.queryByText("Performance")).toBeNull();
        expect(screen.queryByPlaceholderText("Search leads…")).toBeTruthy();

        await clickTab(/Overview/i);
        await settle();
        expect(router.state.location.pathname).toBe("/app/campaigns/camp-1");
        expect(screen.queryByPlaceholderText("Search leads…")).toBeNull();
        expect(screen.queryByText("Performance")).toBeTruthy();
    });

    it("puts every navigation back at the top of the content panel", async () => {
        hang = null;
        await mountDashboard();
        await settle();
        contentPanel().scrollTop = 900;

        await clickTab(/Leads/i);
        await settle();
        expect(contentPanel().scrollTop).toBe(0);
    });

    it("shows a loading state instead of an empty panel when a page suspends", async () => {
        // The Steps tab reads its data with useSuspenseQuery. Navigate while
        // that request is still open: something has to render, or the panel
        // sits blank until the user reloads.
        hang = /\/steps$/;
        await mountDashboard();
        await settle();

        await clickTab(/Steps/i);
        await settle();
        const main = document.querySelector("main");
        expect(main?.querySelector(".animate-pulse")).toBeTruthy();
        hang = null;
    });
});
