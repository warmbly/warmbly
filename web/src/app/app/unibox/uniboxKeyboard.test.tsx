// The unibox is the screen that answers the list shortcuts (#484). It used to
// answer them from its own document-level keydown listener, which is why the
// `?` modal and the dispatcher could disagree about whether j and k worked:
// they did here and nowhere else, while the modal showed them everywhere.
//
// Now the keys live in the global registry and this screen registers what they
// mean. That is a behaviour-preserving refactor only if these still work, so
// they are pinned through the real shell.

import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { screen, act, fireEvent } from "@testing-library/react";
import { useAppStore } from "@/stores";
import { visibleShortcuts } from "@/hooks/useKeyboardShortcuts";
import {
    installLayoutShims,
    mount,
    resetScrollTops,
    ROWS,
    setViewportWidth,
    settle,
    SUITE,
} from "./uniboxHarness";

beforeAll(() => {
    installLayoutShims();
    setViewportWidth(1512);
});

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

function press(key: string, init: Record<string, unknown> = {}, target?: Element) {
    act(() => {
        fireEvent.keyDown(target ?? document.body, { key, ...init });
    });
}

const selected = () => useAppStore.getState().selectedThreadId;

describe("unibox list shortcuts (#484)", SUITE, () => {
    beforeEach(() => {
        resetScrollTops();
        useAppStore.setState({ selectedThreadId: null, navCollapsed: false });
        useAppStore.getState().clearSequence();
    });

    it("moves, jumps to the ends, opens and deselects", async () => {
        await mount("/app/unibox/all");
        await settle();

        // The modal is allowed to show these only because this screen answers
        // them; on a screen that does not, the same rows are hidden.
        expect(visibleShortcuts("list").map((r) => r.keys.join(""))).toContain("j");

        press("j");
        expect(selected()).toBe(ROWS[0].thread_id);
        press("j");
        expect(selected()).toBe(ROWS[1].thread_id);
        press("k");
        expect(selected()).toBe(ROWS[0].thread_id);

        press("G", { shiftKey: true });
        expect(selected()).toBe(ROWS[ROWS.length - 1].thread_id);

        // `g` then `g`, which the sequence rule now lets through: the single
        // `g` no longer resolves to anything on its own.
        press("g");
        press("g");
        expect(selected()).toBe(ROWS[0].thread_id);

        press("Escape");
        expect(selected()).toBeNull();

        // From no selection the keys take the end they point at: j the top of
        // the list, k the bottom. Anything else skips the row the user is
        // looking at.
        press("k");
        expect(selected()).toBe(ROWS[ROWS.length - 1].thread_id);

        press("Escape");
        // Enter takes the top of the list when nothing is selected yet.
        press("Enter");
        expect(selected()).toBe(ROWS[0].thread_id);
    });

    it("gives `/` to the list's own search box, and then stays out of the way", async () => {
        await mount("/app/unibox/all");
        await settle();

        const search = screen.getByPlaceholderText(/^Search/i);
        press("/");
        expect(document.activeElement).toBe(search);

        // The keys must not fire from inside the box they just focused.
        press("j", {}, search);
        expect(selected()).toBeNull();

        // Escape hands the keyboard back rather than reaching the list, which
        // would clear a selection the user cannot see being cleared.
        act(() => {
            fireEvent.keyDown(search, { key: "Escape" });
        });
        expect(document.activeElement).not.toBe(search);
    });

    it("labels the open conversation with `c`, and only while one is open", async () => {
        await mount("/app/unibox/all");
        await settle();

        // No thread open: the row is not offered.
        expect(
            visibleShortcuts("list").some((r) => r.keys.join("") === "c"),
        ).toBe(false);

        await act(async () => {
            fireEvent.click(screen.getByText(ROWS[0].subject).closest("button")!);
        });
        await settle();

        expect(visibleShortcuts("list").some((r) => r.keys.join("") === "c")).toBe(true);
        press("c");
        await settle();
        expect(screen.getByPlaceholderText(/label/i)).toBeTruthy();
    });
});
