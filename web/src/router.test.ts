import { describe, expect, it, vi } from "vitest";
import { router } from "./router";

vi.mock("./lib/helper/getToken", () => ({ default: () => null }));

type Guard = (context: { location: { href: string; searchStr: string } }) => void;
type Redirect = { options: { to: string; search: Record<string, string> } };

function routeRedirect(path: "/" | "/oauth" | "/onboarding", href: string): Redirect {
    const route = router.routesByPath[path];
    const beforeLoad = (route.options.beforeLoad ?? route.parentRoute?.options.beforeLoad) as Guard;
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

describe("code-confirmation paths", () => {
    it("forward to the current flow without carrying the query", () => {
        for (const [path, to] of [["/auth/login/confirm", "/auth/login"], ["/auth/register/confirm", "/auth/register"]] as const) {
            const beforeLoad = router.routesByPath[path].options.beforeLoad as Guard;
            let redirected: { options: { href: string } } | undefined;
            try {
                beforeLoad({ location: { href: `${path}?session=jwt&to=a%40b.c`, searchStr: "?session=jwt&to=a%40b.c" } });
            } catch (error) {
                redirected = error as { options: { href: string } };
            }
            expect(redirected?.options.href).toBe(to);
        }
    });
});

describe("store order routes", () => {
    const found = (path: string) => router.getMatchedRoutes(path)[2]?.fullPath;

    it("opens the order history as its own page, beside the buy layout", () => {
        expect(found("/app/emails/buy/orders")).toBe("/app/emails/buy/orders");
    });

    it("keeps one order inside the buy layout", () => {
        expect(found("/app/emails/buy/orders/0b6f2c4e-1111-4222-8333-444455556666")).toBe("/app/emails/buy/orders/$id");
        expect(found("/app/emails/buy")).toBe("/app/emails/buy/");
    });

    it("routes the agency's purchased infrastructure", () => {
        expect(found("/app/agency/store")).toBe("/app/agency/store");
    });
});
