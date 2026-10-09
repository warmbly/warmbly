import { describe, expect, it, vi } from "vitest";
import { router } from "./router";

vi.mock("./lib/helper/getToken", () => ({ default: () => null }));

type Guard = (context: { location: { href: string; searchStr: string } }) => void;
type Redirect = { options: { to: string; search: Record<string, string> } };

function routeRedirect(path: "/" | "/oauth" | "/onboarding", href: string): Redirect {
    const beforeLoad = router.routesByPath[path].options.beforeLoad as Guard;
    try {
        beforeLoad({ location: { href, searchStr: new URL(href, window.location.origin).search } });
    } catch (error) {
        return error as Redirect;
    }
    throw new Error("Expected route to redirect");
}

describe("acquisition route redirects", () => {
    it("keeps acquisition through the root entrypoint", () => {
        const redirected = routeRedirect("/", "/?utm_source=reddit&wb_ref=www.reddit.com&wb_lp=/pricing&code=private");
        expect(redirected.options.to).toBe("/app/emails");
        expect(redirected.options.search).toEqual({ utm_source: "reddit", wb_ref: "www.reddit.com", wb_lp: "/pricing" });
    });

    it("keeps acquisition available on sign-in as well as in the existing next destination", () => {
        for (const path of ["/oauth", "/onboarding"] as const) {
            const href = `${path}?wb_ref=www.google.com&wb_lp=/`;
            const redirected = routeRedirect(path, href);
            expect(redirected.options.to).toBe("/auth/login");
            expect(redirected.options.search).toEqual({ wb_ref: "www.google.com", wb_lp: "/", next: href });
        }
    });
});
