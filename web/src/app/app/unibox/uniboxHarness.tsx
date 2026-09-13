// Shared harness for the unibox route tests.
//
// Both suites mount the REAL shell (RootAppLayout -> AppShell -> RouteBoundary
// -> Suspense -> Outlet) around the real unibox route, because what they are
// pinning is structural: which components survive a click, and which state
// survives with them. A shallow render of the page would answer neither.
//
// vi.mock factories are hoisted above imports, so they stay in each test file
// and reach `route` through a lazy `await import` of this module.

import React from "react";
import { act, render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// jsdom has no layout: scrollTop is a hard 0, the height properties do not
// exist, and getBoundingClientRect is all zeroes. Back them with real values so
// an offset (and a pointer drag) is something a test can set and read.
let tops = new WeakMap<Element, number>();

export function installLayoutShims() {
    Object.defineProperty(Element.prototype, "scrollTop", {
        configurable: true,
        get(this: Element) {
            return tops.get(this) ?? 0;
        },
        set(this: Element, value: number) {
            tops.set(this, value);
        },
    });
    Object.defineProperty(Element.prototype, "clientHeight", {
        configurable: true,
        get: () => 400,
    });
    Object.defineProperty(Element.prototype, "scrollHeight", {
        configurable: true,
        get: () => 4000,
    });
    (Element.prototype as unknown as { scrollTo: () => void }).scrollTo = () => {};
    (Element.prototype as unknown as { scrollIntoView: () => void }).scrollIntoView = () => {};
}

export function resetScrollTops() {
    tops = new WeakMap<Element, number>();
}

// jsdom implements matchMedia but never evaluates a query, so every one of them
// reports `matches: false`. A layout that branches on `lg` would therefore only
// ever be tested in its narrow form. Evaluate min-/max-width against a width the
// test sets, and keep window.innerWidth in step for the hooks that read it.
export function setViewportWidth(width: number) {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
    const listeners = new Set<() => void>();
    Object.defineProperty(window, "matchMedia", {
        configurable: true,
        writable: true,
        value: (query: string): MediaQueryList => {
            const min = /min-width:\s*(\d+)px/.exec(query);
            const max = /max-width:\s*(\d+)px/.exec(query);
            const matches =
                (!min || width >= Number(min[1])) && (!max || width <= Number(max[1]));
            return {
                matches,
                media: query,
                onchange: null,
                addEventListener: (_: string, fn: () => void) => listeners.add(fn),
                removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
                addListener: () => {},
                removeListener: () => {},
                dispatchEvent: () => false,
            } as unknown as MediaQueryList;
        },
    });
}

const EMPTY_LIST = { data: [], pagination: { total: 0, next_cursor: null, has_more: false } };

export const ROWS = Array.from({ length: 12 }, (_, i) => ({
    id: `msg-${i}`,
    email_id: "mbox-1",
    thread_id: `thread-${i}`,
    from_addr: [`Sender ${i} <s${i}@example.com>`],
    to_addr: ["me@warmbly.com"],
    subject: `Subject ${i}`,
    snippet: `Snippet ${i}`,
    internal_date: new Date(Date.now() - i * 3600e3).toISOString(),
    seen: true,
    message_count: 1,
    has_unread: false,
    labels: [],
}));

export function route(url: string): unknown {
    if (url === "/auth/me" || url === "/me") {
        return {
            id: "u1", email: "d@w.com", first_name: "D", last_name: "W",
            onboarding_completed_at: new Date().toISOString(),
            tags: [], categories: [], folders: [], roles: [],
        };
    }
    // billing_enabled:false unlocks every feature gate, so the inbox renders
    // for real instead of behind the upgrade overlay.
    if (url.startsWith("/auth/config")) {
        return {
            captcha: false, password_login: true, login_code: "off",
            registration: "invite_only", invites_required: true,
            email_verification: false, mail_delivers: false, passkeys: false,
            providers: [], self_hosted: true, billing_enabled: false,
            setup_required: false, docs_url: "",
        };
    }
    if (url.startsWith("/organization")) return [{ id: "org-1", name: "Org", slug: "org", role: "owner" }];
    if (url.startsWith("/subscription/credits")) {
        return { monthly_balance: 100, monthly_allowance: 100, purchased_balance: 0, spent_today: 0, spent_week: 0, spent_month: 0 };
    }
    if (url.startsWith("/subscription")) return { plan: { name: "Pro" }, status: "active" };
    if (url.startsWith("/unibox/overview")) {
        return { total: ROWS.length, unread: 0, awaiting_reply: 0, snoozed: 0, today: 0, week: 0, mailboxes: [], tags: [], categories: [], folders: [] };
    }
    if (url.startsWith("/unibox/count")) return { count: 0 };
    if (url.startsWith("/unibox/thread")) {
        const id = /thread\/([^/?]+)/.exec(url)?.[1];
        const row = ROWS.find((r) => r.thread_id === id) ?? ROWS[0];
        return { data: [{ ...row, seen: true }], pagination: { has_more: false, next_cursor: null } };
    }
    if (url === "/unibox" || url.startsWith("/unibox?")) {
        return { data: ROWS, pagination: { has_more: false, next_cursor: null } };
    }
    if (url.startsWith("/analytics")) return { summary: {}, steps: [], data: [] };
    if (url.startsWith("/advisor")) return { findings: [], data: [], total: 0 };
    return EMPTY_LIST;
}

function Elsewhere() {
    return <div>Somewhere else</div>;
}

export async function mount(initial = "/app/unibox/all") {
    const RootAppLayout = (await import("../layout")).default;
    const UniboxPage = (await import("./page")).default;
    const router = createMemoryRouter(
        [
            {
                path: "/app",
                element: <RootAppLayout />,
                children: [
                    {
                        path: "unibox/:scope?/:threadId?",
                        element: <UniboxPage />,
                        handle: { stableParams: ["scope", "threadId"] },
                    },
                    { path: "analytics", element: <Elsewhere /> },
                ],
            },
        ],
        { initialEntries: [initial] },
    );
    render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return router;
}

export async function settle() {
    await act(async () => {
        await new Promise((r) => setTimeout(r, 300));
    });
}
