import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { bootDashboard, requireDashboardToken, resetBoot } from "./boot";
import getToken from "./helper/getToken";
import { useAppStore } from "@/stores/useAppStore";

vi.mock("./helper/getToken", () => ({ default: vi.fn() }));
vi.mock("./api/client/app/organizations/switchOrganization", () => ({ default: vi.fn().mockResolvedValue(undefined) }));

beforeEach(() => {
    resetBoot();
    vi.mocked(getToken).mockReturnValue(null);
});
afterEach(() => vi.restoreAllMocks());

function loginRedirect(href: string) {
    const url = new URL(href, window.location.origin);
    try {
        requireDashboardToken({ pathname: url.pathname, href });
    } catch (error) {
        return error as { options: { to: string; search: Record<string, string> } };
    }
    throw new Error("Expected an unauthenticated redirect");
}

describe("dashboard acquisition redirect", () => {
    it("keeps acquisition at login after a dashboard link, without copying unrelated parameters", () => {
        const redirected = loginRedirect("/app/emails?utm_source=reddit&wb_lp=/pricing&wb_ref=www.reddit.com&code=private");
        expect(redirected.options.to).toBe("/auth/login");
        expect(redirected.options.search).toEqual({ utm_source: "reddit", wb_lp: "/pricing", wb_ref: "www.reddit.com" });
    });

    it("still retains the one-time Slack destination alongside acquisition", () => {
        const href = "/app/slack/link?code=one-time&wb_ref=www.google.com";
        expect(loginRedirect(href).options.search).toEqual({ wb_ref: "www.google.com", next: href });
    });

    it("does not add attribution for an untagged visit", () => {
        expect(loginRedirect("/app/emails").options.search).toEqual({});
    });

    it("retains acquisition when a remembered session expires during boot", async () => {
        vi.mocked(getToken).mockReturnValue({} as NonNullable<ReturnType<typeof getToken>>);
        const client = new QueryClient();
        vi.spyOn(client, "prefetchQuery").mockResolvedValue(undefined);
        vi.spyOn(client, "ensureQueryData").mockRejectedValue({ status: 401 });
        await expect(bootDashboard(client, { pathname: "/app/emails", href: "/app/emails?wb_ref=www.reddit.com&wb_lp=/pricing" })).rejects.toMatchObject({
            options: { to: "/auth/login", search: { wb_ref: "www.reddit.com", wb_lp: "/pricing" } },
        });
        client.clear();
    });
});

describe("agency landing", () => {
    const org = (id: string) => ({ id, name: id, role: "owner", created_at: new Date() });
    const agency = { id: "ag", name: "Northwind Growth", client_count: 2 };

    function bootWith(agencies: unknown[], orgs = [org("a"), org("b")]) {
        vi.mocked(getToken).mockReturnValue({} as NonNullable<ReturnType<typeof getToken>>);
        const client = new QueryClient();
        vi.spyOn(client, "prefetchQuery").mockResolvedValue(undefined);
        vi.spyOn(client, "ensureQueryData").mockImplementation((async (opts: { queryKey: readonly unknown[] }) => {
            const key = opts.queryKey.join("/");
            if (key === "organizations/list") return orgs;
            if (key === "agency/list") return { data: agencies, billing_enabled: false };
            if (key === "auth/me") return { onboarding_completed_at: "2026-01-01" };
            return undefined;
        }) as QueryClient["ensureQueryData"]);
        return client;
    }

    beforeEach(() => {
        useAppStore.setState({ currentOrganization: null, organizations: [] });
        vi.mocked(window.localStorage.getItem).mockReturnValue(null);
    });

    it("sends agency staff with no remembered workspace to the agency, not the picker", async () => {
        const client = bootWith([agency]);
        await expect(bootDashboard(client, { pathname: "/app/emails", href: "/app/emails" })).rejects.toMatchObject({ options: { to: "/app/agency" } });
    });

    it("still sends everyone else to the picker", async () => {
        const client = bootWith([]);
        await expect(bootDashboard(client, { pathname: "/app/emails", href: "/app/emails" })).rejects.toMatchObject({ options: { to: "/select-org" } });
    });

    it("renders agency pages with no workspace picked", async () => {
        const client = bootWith([agency]);
        await expect(bootDashboard(client, { pathname: "/app/agency/clients", href: "/app/agency/clients" })).resolves.toBeUndefined();
        expect(useAppStore.getState().currentOrganization).toBeNull();
    });

    it("returns to the agency view when the app was left there", async () => {
        useAppStore.setState({ currentOrganization: org("a") });
        vi.mocked(window.localStorage.getItem).mockImplementation((k) => (k === "warmbly.view" ? "agency" : null));
        const client = bootWith([agency]);
        await expect(bootDashboard(client, { pathname: "/app/emails", href: "/app/emails" })).rejects.toMatchObject({ options: { to: "/app/agency" } });
    });

    it("keeps a link to a workspace page where it pointed", async () => {
        useAppStore.setState({ currentOrganization: org("a") });
        vi.mocked(window.localStorage.getItem).mockImplementation((k) => (k === "warmbly.view" ? "agency" : null));
        const client = bootWith([agency]);
        await expect(bootDashboard(client, { pathname: "/app/campaigns", href: "/app/campaigns" })).resolves.toBeUndefined();
    });
});
