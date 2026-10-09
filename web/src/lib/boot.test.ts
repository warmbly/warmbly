import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { bootDashboard, requireDashboardToken, resetBoot } from "./boot";
import getToken from "./helper/getToken";

vi.mock("./helper/getToken", () => ({ default: vi.fn() }));

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
