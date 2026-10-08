// The search box used to be scoped state: typing in Inbox and switching view
// cleared it, which is right, but it also meant there was no way to widen a
// search that found nothing without retyping it. The query now lives on the
// page so "Search all mail" can change scope and keep it, and a scope change
// the reader makes themselves still clears it.

import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { screen, act, fireEvent, waitFor } from "@testing-library/react";
import { useAppStore } from "@/stores";
import {
    installLayoutShims,
    mount,
    resetScrollTops,
    setViewportWidth,
    settle,
    SUITE,
} from "./uniboxHarness";

const searchRequests = vi.hoisted((): string[] => []);

beforeAll(() => {
    installLayoutShims();
    setViewportWidth(1512);
});

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: { url?: string }) => {
        if (cfg.url === "/unibox" || cfg.url?.startsWith("/unibox?")) {
            searchRequests.push(cfg.url);
        }
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

function box(): HTMLInputElement {
    return screen.getByPlaceholderText(/name, address, or any word/i) as HTMLInputElement;
}

async function type(value: string) {
    await act(async () => {
        fireEvent.change(box(), { target: { value } });
    });
    // The box is debounced into the query, so the request needs the timer.
    await settle();
    await settle();
}

function lastQuery(): string | null {
    const url = searchRequests[searchRequests.length - 1];
    return url ? new URL(url, "https://test.local").searchParams.get("subject") : null;
}

describe("unibox search", SUITE, () => {
    beforeEach(() => {
        searchRequests.length = 0;
        resetScrollTops();
        useAppStore.setState({ selectedThreadId: null, navCollapsed: false });
    });

    it("does not broaden an explicit tag with no matching mailboxes into an all-mail request", async () => {
        await mount("/app/unibox/tag?ref=unassigned-tag");
        await settle();
        expect(searchRequests).toEqual([]);
        expect(screen.queryByText("Subject 3")).toBeNull();
    });

    it("sends what was typed as the free-text param", async () => {
        await mount("/app/unibox/all");
        await settle();

        await type("Subject 3");
        await waitFor(() => expect(lastQuery()).toBe("Subject 3"));
    });

    it("offers to widen a search that found nothing in a folder, keeping the query", async () => {
        const router = await mount("/app/unibox/inbox");
        await settle();

        await type("nothing matches this");
        await waitFor(() => expect(screen.getByText("No matches")).toBeTruthy());

        // The offer only exists because the reader is not already in All mail.
        const widen = screen.getByRole("button", { name: /search all mail/i });
        searchRequests.length = 0;
        await act(async () => {
            fireEvent.click(widen);
        });
        await settle();

        // Scope widened...
        expect(router.state.location.pathname).toBe("/app/unibox/all");
        // ...and the query survived it, which is the whole point.
        expect(box().value).toBe("nothing matches this");
        await waitFor(() => expect(lastQuery()).toBe("nothing matches this"));
        expect(
            searchRequests.every(
                (u) => !new URL(u, "https://test.local").searchParams.has("folder"),
            ),
        ).toBe(true);
    });

    it("has nothing to widen to when already searching all mail", async () => {
        await mount("/app/unibox/all");
        await settle();

        await type("nothing matches this");
        await waitFor(() => expect(screen.getByText("No matches")).toBeTruthy());
        expect(screen.queryByRole("button", { name: /search all mail/i })).toBeNull();
    });

    it("clears the query on a scope change the reader made", async () => {
        const router = await mount("/app/unibox/all");
        await settle();

        await type("Subject 3");
        await waitFor(() => expect(box().value).toBe("Subject 3"));

        await act(async () => {
            await router.navigate({ to: "/app/unibox/{-$scope}/{-$threadId}", params: { scope: "inbox", threadId: undefined } });
        });
        await settle();

        expect(box().value).toBe("");
    });
});
