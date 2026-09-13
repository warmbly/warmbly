// Issue #396: opening an email sent the conversation list back to the top.
//
// The cause was structural, not visual: the shell keyed its route boundary on
// the pathname, and the unibox puts the open thread IN the pathname, so every
// click tore the page down and built a new one. This mounts the real shell
// (RootAppLayout -> AppShell -> RouteBoundary -> Suspense -> Outlet) around the
// real unibox route and pins both halves of the fix: the list survives the
// click, and a remembered offset is put back whenever something else does zero
// it (the mobile pane, or leaving the inbox and coming back).

import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { screen, act, fireEvent } from "@testing-library/react";
import {
    installLayoutShims,
    mount,
    resetScrollTops,
    settle,
    SUITE,
} from "./uniboxHarness";

beforeAll(installLayoutShims);

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: { url?: string }) => {
        const { route } = await import("./uniboxHarness");
        return route(String(cfg?.url ?? ""));
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

function scroller(): HTMLElement {
    const row = document.querySelector("[data-thread-id]");
    const el = row?.closest<HTMLElement>(".overflow-y-auto");
    if (!el) throw new Error("conversation list scroll container not found");
    return el;
}

async function scrollTo(top: number) {
    const el = scroller();
    el.scrollTop = top;
    await act(async () => {
        fireEvent.scroll(el);
    });
}

describe("unibox scroll position", SUITE, () => {
    // The fake offsets are per element and the hook's own memory is per list
    // identity, so tests reset the first and take a scope of their own for the
    // second. Otherwise a passing assertion could be the previous test's.
    beforeEach(() => {
        resetScrollTops();
    });

    it("keeps the list where it was when a thread is opened", async () => {
        const router = await mount("/app/unibox/all");
        await settle();
        expect(screen.queryByText("Subject 4")).toBeTruthy();

        const before = scroller();
        await scrollTo(1200);

        await act(async () => {
            fireEvent.click(screen.getByText("Subject 4").closest("button")!);
        });
        await settle();

        expect(router.state.location.pathname).toBe("/app/unibox/all/thread-4");
        // Same DOM node: the page was never torn down, which is the whole fix.
        expect(scroller()).toBe(before);
        expect(scroller().scrollTop).toBe(1200);
    });

    it("keeps what the user typed into the list search when a thread is opened", async () => {
        await mount("/app/unibox/unread");
        await settle();

        const search = screen.getByPlaceholderText(/^Search unread/i) as HTMLInputElement;
        await act(async () => {
            fireEvent.change(search, { target: { value: "invoice" } });
        });
        await settle();

        await act(async () => {
            fireEvent.click(screen.getByText("Subject 2").closest("button")!);
        });
        await settle();

        expect(
            (screen.getByPlaceholderText(/^Search unread/i) as HTMLInputElement).value,
        ).toBe("invoice");
    });

    it("puts a remembered offset back after the pane is hidden and shown again", async () => {
        // Below `md` the list is display:none while a thread is open, and the
        // browser zeroes a hidden scroller without firing a scroll event. Same
        // thing here: move the offset behind the component's back, then render.
        await mount("/app/unibox/today");
        await settle();
        await scrollTo(700);

        await act(async () => {
            fireEvent.click(screen.getByText("Subject 3").closest("button")!);
        });
        await settle();
        scroller().scrollTop = 0;

        // The thread pane's back link, the mobile way back to the list.
        const back = screen
            .getAllByRole("button", { name: "Inbox" })
            .find((b) => b.className.includes("md:hidden"))!;
        await act(async () => {
            fireEvent.click(back);
        });
        await settle();

        expect(scroller().scrollTop).toBe(700);
    });

    it("puts a remembered offset back when the inbox is re-entered", async () => {
        const router = await mount("/app/unibox/week");
        await settle();
        await scrollTo(900);

        await act(async () => {
            await router.navigate("/app/analytics");
        });
        await settle();
        expect(screen.queryByText("Somewhere else")).toBeTruthy();

        await act(async () => {
            await router.navigate("/app/unibox/week");
        });
        await settle();

        expect(scroller().scrollTop).toBe(900);
    });
});
