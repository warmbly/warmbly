// "<Page> | Warmbly Admin" on every navigation. The nav model is the source
// for list pages; detail routes get a small map here. Anything unmapped is
// "Not found", which is what the catch-all route renders anyway.

import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { NAV_GROUPS } from "@/components/layout/Sidebar";

const BRAND = "Warmbly Admin";

const NAV_TITLES: Record<string, string> = Object.fromEntries(
    NAV_GROUPS.flatMap((g) => g.items.map((item) => [item.to, item.label])),
);

const STATIC_TITLES: Record<string, string> = {
    ...NAV_TITLES,
    "/workers/new": "New worker",
    "/warmup-content/overview": "Warmup content overview",
    "/warmup-content/library": "Warmup content library",
    "/warmup-content/jobs": "Warmup content jobs",
    "/auth/login": "Sign in",
};

const PARAM_ROUTES: Array<{ pattern: RegExp; title: string }> = [
    { pattern: /^\/users\/[^/]+$/, title: "User" },
    { pattern: /^\/organizations\/[^/]+$/, title: "Organization" },
    { pattern: /^\/workers\/[^/]+$/, title: "Worker" },
];

export function titleForPath(pathname: string): string {
    const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
    const label =
        STATIC_TITLES[path] ?? PARAM_ROUTES.find((r) => r.pattern.test(path))?.title ?? "Not found";
    return `${label} | ${BRAND}`;
}

export function useDocumentTitle() {
    const { pathname } = useLocation();
    useEffect(() => {
        document.title = titleForPath(pathname);
    }, [pathname]);
}
